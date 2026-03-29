package harness

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/tjohnson/maestro/internal/config"
)

const dockerAllowlistProxyHost = "host.docker.internal"

var dockerAllowlistProxyRegistry sync.Map

type dockerAllowlistProxy struct {
	username  string
	password  string
	listener  net.Listener
	transport *http.Transport
	allow     []string
}

func ensureDockerAllowlistProxy(allow []string) (*dockerAllowlistProxy, error) {
	normalized := make([]string, 0, len(allow))
	for _, entry := range allow {
		normalized = append(normalized, config.NormalizeDockerNetworkAllowEntry(entry))
	}
	key := strings.Join(normalized, ",")
	if existing, ok := dockerAllowlistProxyRegistry.Load(key); ok {
		return existing.(*dockerAllowlistProxy), nil
	}

	proxy, err := newDockerAllowlistProxy(normalized)
	if err != nil {
		return nil, err
	}
	actual, loaded := dockerAllowlistProxyRegistry.LoadOrStore(key, proxy)
	if loaded {
		_ = proxy.listener.Close()
		return actual.(*dockerAllowlistProxy), nil
	}
	return proxy, nil
}

func newDockerAllowlistProxy(allow []string) (*dockerAllowlistProxy, error) {
	listener, err := net.Listen("tcp4", "0.0.0.0:0")
	if err != nil {
		return nil, fmt.Errorf("listen for docker allowlist proxy: %w", err)
	}
	username, err := randomHex(12)
	if err != nil {
		_ = listener.Close()
		return nil, err
	}
	password, err := randomHex(24)
	if err != nil {
		_ = listener.Close()
		return nil, err
	}

	proxy := &dockerAllowlistProxy{
		username: username,
		password: password,
		listener: listener,
		allow:    allow,
		transport: &http.Transport{
			Proxy:                 nil,
			ForceAttemptHTTP2:     false,
			DialContext:           (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			TLSHandshakeTimeout:   30 * time.Second,
			ResponseHeaderTimeout: 60 * time.Second,
			ExpectContinueTimeout: time.Second,
			IdleConnTimeout:       90 * time.Second,
		},
	}
	server := &http.Server{
		Handler:           proxy,
		ReadHeaderTimeout: 15 * time.Second,
	}
	go func() {
		_ = server.Serve(listener)
	}()
	return proxy, nil
}

func (p *dockerAllowlistProxy) containerURL() string {
	port := p.listener.Addr().(*net.TCPAddr).Port
	return fmt.Sprintf("http://%s:%s@%s:%d", p.username, p.password, dockerAllowlistProxyHost, port)
}

func (p *dockerAllowlistProxy) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if !p.authorized(req) {
		w.Header().Set("Proxy-Authenticate", `Basic realm="maestro-egress"`)
		http.Error(w, "proxy authentication required", http.StatusProxyAuthRequired)
		return
	}
	if req.Method == http.MethodConnect {
		p.handleConnect(w, req)
		return
	}
	p.handleHTTP(w, req)
}

func (p *dockerAllowlistProxy) authorized(req *http.Request) bool {
	header := strings.TrimSpace(req.Header.Get("Proxy-Authorization"))
	if !strings.HasPrefix(header, "Basic ") {
		return false
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(strings.TrimPrefix(header, "Basic ")))
	if err != nil {
		return false
	}
	user, pass, ok := strings.Cut(string(decoded), ":")
	if !ok {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(user), []byte(p.username)) == 1 &&
		subtle.ConstantTimeCompare([]byte(pass), []byte(p.password)) == 1
}

func (p *dockerAllowlistProxy) handleHTTP(w http.ResponseWriter, req *http.Request) {
	host, err := proxyTargetHost(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !p.allowed(host) {
		http.Error(w, "blocked by docker network allowlist", http.StatusForbidden)
		return
	}

	outReq := req.Clone(req.Context())
	outReq.RequestURI = ""
	outReq.Header = req.Header.Clone()
	removeHopByHopHeaders(outReq.Header)
	outReq.Header.Del("Proxy-Authorization")

	resp, err := p.transport.RoundTrip(outReq)
	if err != nil {
		http.Error(w, fmt.Sprintf("proxy request failed: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	removeHopByHopHeaders(resp.Header)
	copyHeader(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func (p *dockerAllowlistProxy) handleConnect(w http.ResponseWriter, req *http.Request) {
	host, address, err := proxyConnectAddress(req.Host)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !p.allowed(host) {
		http.Error(w, "blocked by docker network allowlist", http.StatusForbidden)
		return
	}

	dst, err := (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext(req.Context(), "tcp", address)
	if err != nil {
		http.Error(w, fmt.Sprintf("proxy connect failed: %v", err), http.StatusBadGateway)
		return
	}

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		_ = dst.Close()
		http.Error(w, "proxy does not support hijacking", http.StatusInternalServerError)
		return
	}
	clientConn, buf, err := hijacker.Hijack()
	if err != nil {
		_ = dst.Close()
		http.Error(w, fmt.Sprintf("proxy hijack failed: %v", err), http.StatusInternalServerError)
		return
	}

	if _, err := clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		_ = clientConn.Close()
		_ = dst.Close()
		return
	}
	if buffered := buf.Reader.Buffered(); buffered > 0 {
		if _, err := io.CopyN(dst, buf, int64(buffered)); err != nil {
			_ = clientConn.Close()
			_ = dst.Close()
			return
		}
	}

	go proxyCopyAndClose(dst, clientConn)
	go proxyCopyAndClose(clientConn, dst)
}

func proxyCopyAndClose(dst net.Conn, src net.Conn) {
	_, _ = io.Copy(dst, src)
	_ = dst.Close()
	_ = src.Close()
}

func proxyTargetHost(req *http.Request) (string, error) {
	if req.URL != nil && strings.TrimSpace(req.URL.Host) != "" {
		host, _, err := splitHostPortDefault(req.URL.Host, defaultProxyPort(req.URL.Scheme))
		return host, err
	}
	if strings.TrimSpace(req.Host) != "" {
		host, _, err := splitHostPortDefault(req.Host, defaultProxyPort(req.URL.Scheme))
		return host, err
	}
	return "", fmt.Errorf("proxy request target host is required")
}

func proxyConnectAddress(raw string) (string, string, error) {
	return splitHostPortDefault(raw, "443")
}

func splitHostPortDefault(raw string, defaultPort string) (string, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", fmt.Errorf("target host is required")
	}
	if strings.Contains(raw, "://") {
		return "", "", fmt.Errorf("target host must not include a URL scheme")
	}
	if host, port, err := net.SplitHostPort(raw); err == nil {
		host = strings.Trim(strings.TrimSpace(host), "[]")
		if host == "" {
			return "", "", fmt.Errorf("target host is empty")
		}
		return normalizeProxyHost(host), net.JoinHostPort(host, port), nil
	}
	host := strings.Trim(strings.TrimSpace(raw), "[]")
	if host == "" {
		return "", "", fmt.Errorf("target host is empty")
	}
	return normalizeProxyHost(host), net.JoinHostPort(host, defaultPort), nil
}

func defaultProxyPort(scheme string) string {
	if strings.EqualFold(strings.TrimSpace(scheme), "https") {
		return "443"
	}
	return "80"
}

func normalizeProxyHost(host string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
}

func (p *dockerAllowlistProxy) allowed(host string) bool {
	host = normalizeProxyHost(host)
	hostIP := net.ParseIP(strings.Trim(host, "[]"))
	for _, entry := range p.allow {
		switch {
		case strings.HasPrefix(entry, "*."):
			suffix := strings.TrimPrefix(entry, "*.")
			if strings.HasSuffix(host, "."+suffix) && host != suffix {
				return true
			}
		case hostIP != nil && net.ParseIP(strings.Trim(entry, "[]")) != nil:
			if hostIP.Equal(net.ParseIP(strings.Trim(entry, "[]"))) {
				return true
			}
		case host == entry:
			return true
		}
	}
	return false
}

func removeHopByHopHeaders(header http.Header) {
	for _, name := range []string{
		"Connection",
		"Keep-Alive",
		"Proxy-Authenticate",
		"Proxy-Authorization",
		"Proxy-Connection",
		"Te",
		"Trailer",
		"Transfer-Encoding",
		"Upgrade",
	} {
		for _, token := range header.Values(name) {
			for _, value := range strings.Split(token, ",") {
				header.Del(strings.TrimSpace(value))
			}
		}
		header.Del(name)
	}
}

func copyHeader(dst http.Header, src http.Header) {
	for key, values := range src {
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func appendNoProxy(existing string, values ...string) string {
	seen := map[string]struct{}{}
	parts := []string{}
	for _, item := range strings.Split(existing, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		parts = append(parts, item)
	}
	for _, item := range values {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		parts = append(parts, item)
	}
	return strings.Join(parts, ",")
}

func randomHex(bytesLen int) (string, error) {
	buf := make([]byte, bytesLen)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return "", fmt.Errorf("generate random proxy credential: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

var _ http.Handler = (*dockerAllowlistProxy)(nil)
