package harness

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/tjohnson/maestro/internal/config"
)

const (
	dockerReuseManagedLabel = "maestro.reuse.managed=true"
	dockerReuseOwnerLabel   = "maestro.reuse.owner_pid"
	dockerReuseJanitorEvery = 2 * time.Minute
)

var errDockerReusableBusy = fmt.Errorf("docker reusable container busy")

type DockerReuseManager struct {
	dockerBinary string
	ownerPID     int

	mu         sync.Mutex
	containers map[string]*dockerReusableContainer
	closed     bool
	stopCh     chan struct{}
	doneCh     chan struct{}
}

type dockerReusableContainer struct {
	key           string
	mode          string
	profileKey    string
	lineageKey    string
	containerID   string
	containerName string
	active        bool
}

type dockerReusableLease struct {
	key           string
	mode          string
	profileKey    string
	lineageKey    string
	containerID   string
	containerName string
}

func NewDockerReuseManager() (*DockerReuseManager, error) {
	dockerBinary, err := exec.LookPath("docker")
	if err != nil {
		return nil, fmt.Errorf("find docker executable: %w", err)
	}
	manager := &DockerReuseManager{
		dockerBinary: dockerBinary,
		ownerPID:     os.Getpid(),
		containers:   map[string]*dockerReusableContainer{},
		stopCh:       make(chan struct{}),
		doneCh:       make(chan struct{}),
	}
	go manager.janitor()
	return manager, nil
}

func (m *DockerReuseManager) Acquire(ctx context.Context, mode string, profileKey string, lineageKey string, createArgs []string) (*dockerReusableLease, error) {
	if m == nil || mode == "" || mode == config.DockerReuseModeNone {
		return nil, nil
	}
	key := reusableContainerKey(mode, profileKey, lineageKey)

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, fmt.Errorf("docker reuse manager closed")
	}
	if container, ok := m.containers[key]; ok {
		if container.active {
			m.mu.Unlock()
			return nil, errDockerReusableBusy
		}
		container.active = true
		lease := &dockerReusableLease{
			key:           key,
			mode:          container.mode,
			profileKey:    container.profileKey,
			lineageKey:    container.lineageKey,
			containerID:   container.containerID,
			containerName: container.containerName,
		}
		m.mu.Unlock()
		if err := m.startContainer(ctx, lease.containerName); err != nil {
			_ = m.removeLease(context.Background(), lease)
			return nil, err
		}
		return lease, nil
	}

	containerName := reusableContainerName(mode, profileKey, lineageKey, m.ownerPID)
	container := &dockerReusableContainer{
		key:           key,
		mode:          mode,
		profileKey:    profileKey,
		lineageKey:    lineageKey,
		containerName: containerName,
		active:        true,
	}
	m.containers[key] = container
	m.mu.Unlock()

	createOut, err := m.runDocker(ctx, createArgs...)
	if err != nil {
		m.mu.Lock()
		delete(m.containers, key)
		m.mu.Unlock()
		return nil, err
	}
	container.containerID = strings.TrimSpace(string(createOut))
	lease := &dockerReusableLease{
		key:           key,
		mode:          mode,
		profileKey:    profileKey,
		lineageKey:    lineageKey,
		containerID:   container.containerID,
		containerName: container.containerName,
	}
	if err := m.startContainer(ctx, lease.containerName); err != nil {
		_ = m.removeLease(context.Background(), lease)
		return nil, err
	}
	return lease, nil
}

func (m *DockerReuseManager) Release(ctx context.Context, lease *dockerReusableLease) error {
	if m == nil || lease == nil {
		return nil
	}

	stopErr := m.stopContainer(ctx, lease.containerName)

	m.mu.Lock()
	if container, ok := m.containers[lease.key]; ok {
		container.active = false
	}
	m.mu.Unlock()

	return stopErr
}

func (m *DockerReuseManager) Close() error {
	if m == nil {
		return nil
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	close(m.stopCh)
	containers := make([]*dockerReusableContainer, 0, len(m.containers))
	for _, container := range m.containers {
		containers = append(containers, container)
	}
	m.mu.Unlock()

	<-m.doneCh

	var firstErr error
	for _, container := range containers {
		if err := m.removeContainer(context.Background(), container.containerName); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (m *DockerReuseManager) janitor() {
	defer close(m.doneCh)
	ticker := time.NewTicker(dockerReuseJanitorEvery)
	defer ticker.Stop()
	_ = m.pruneOrphans(context.Background())
	for {
		select {
		case <-ticker.C:
			_ = m.pruneOrphans(context.Background())
		case <-m.stopCh:
			return
		}
	}
}

func (m *DockerReuseManager) pruneOrphans(ctx context.Context) error {
	if m == nil {
		return nil
	}
	out, err := m.runDocker(ctx, "ps", "-a", "--filter", "label="+dockerReuseManagedLabel, "--format", "{{.ID}}\t{{.Label \""+dockerReuseOwnerLabel+"\"}}")
	if err != nil {
		return err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		id, owner, ok := strings.Cut(line, "\t")
		if !ok || strings.TrimSpace(id) == "" {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(owner))
		if err != nil || pid <= 0 {
			continue
		}
		if pid == m.ownerPID || processAlive(pid) {
			continue
		}
		_, _ = m.runDocker(ctx, "rm", "-f", strings.TrimSpace(id))
	}
	return nil
}

func (m *DockerReuseManager) removeLease(ctx context.Context, lease *dockerReusableLease) error {
	if m == nil || lease == nil {
		return nil
	}
	m.mu.Lock()
	delete(m.containers, lease.key)
	m.mu.Unlock()
	return m.removeContainer(ctx, lease.containerName)
}

func (m *DockerReuseManager) removeContainer(ctx context.Context, name string) error {
	if strings.TrimSpace(name) == "" {
		return nil
	}
	_, err := m.runDocker(ctx, "rm", "-f", name)
	return err
}

func (m *DockerReuseManager) startContainer(ctx context.Context, name string) error {
	_, err := m.runDocker(ctx, "start", name)
	return err
}

func (m *DockerReuseManager) stopContainer(ctx context.Context, name string) error {
	if strings.TrimSpace(name) == "" {
		return nil
	}
	_, err := m.runDocker(ctx, "stop", "--time", "1", name)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "is not running") {
		return nil
	}
	return err
}

func (m *DockerReuseManager) runDocker(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, m.dockerBinary, args...)
	cmd.Env = DockerClientEnv(nil)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("docker %s: %w output=%s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func reusableContainerKey(mode string, profileKey string, lineageKey string) string {
	if mode == config.DockerReuseModeLineage && strings.TrimSpace(lineageKey) != "" {
		return mode + "|" + profileKey + "|" + lineageKey
	}
	return mode + "|" + profileKey
}

func reusableContainerName(mode string, profileKey string, lineageKey string, ownerPID int) string {
	key := reusableContainerKey(mode, profileKey, lineageKey)
	sum := sha256.Sum256([]byte(key))
	return fmt.Sprintf("maestro-reuse-%d-%s", ownerPID, hex.EncodeToString(sum[:8]))
}

func processAlive(pid int) bool {
	if pid <= 0 || runtime.GOOS == "windows" {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}
