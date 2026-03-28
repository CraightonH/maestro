package harness

import (
	"os"
	"sort"
)

var childEnvAllowlist = []string{
	"ALL_PROXY",
	"HOME",
	"HTTPS_PROXY",
	"HTTP_PROXY",
	"LANG",
	"LC_ALL",
	"NO_PROXY",
	"PATH",
	"SHELL",
	"SSL_CERT_DIR",
	"SSL_CERT_FILE",
	"TEMP",
	"TERM",
	"TMP",
	"TMPDIR",
	"USER",
	"XDG_CACHE_HOME",
	"XDG_CONFIG_HOME",
	"XDG_DATA_HOME",
	"XDG_RUNTIME_DIR",
	"XDG_STATE_HOME",
}

func MergeEnv(extra map[string]string) []string {
	merged := make(map[string]string, len(childEnvAllowlist)+len(extra))
	for _, key := range childEnvAllowlist {
		if value, ok := os.LookupEnv(key); ok {
			merged[key] = value
		}
	}
	for key, value := range extra {
		merged[key] = value
	}

	keys := make([]string, 0, len(merged))
	for key := range merged {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	env := make([]string, 0, len(keys))
	for _, key := range keys {
		env = append(env, key+"="+merged[key])
	}
	return env
}
