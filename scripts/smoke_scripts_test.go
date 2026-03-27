package scripts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSmokeScriptsKeepTokenEnvNames(t *testing.T) {
	t.Helper()

	cases := []struct {
		path string
		want []string
	}{
		{
			path: "smoke_gitlab.sh",
			want: []string{`token_env: \$MAESTRO_GITLAB_TOKEN`},
		},
		{
			path: "smoke_linear.sh",
			want: []string{`token_env: \$MAESTRO_LINEAR_TOKEN`},
		},
		{
			path: "smoke_multi_source.sh",
			want: []string{
				`token_env: \$MAESTRO_GITLAB_TOKEN`,
				`token_env: \$MAESTRO_LINEAR_TOKEN`,
			},
		},
		{
			path: "smoke_many_sources.sh",
			want: []string{
				`token_env: \$MAESTRO_GITLAB_TOKEN`,
				`token_env: \$MAESTRO_LINEAR_TOKEN`,
			},
		},
		{
			path: "smoke_hermetic.sh",
			want: []string{
				`token_env: \$SMOKE_GITLAB_TOKEN`,
				`token_env: \$SMOKE_LINEAR_TOKEN`,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(".", tc.path))
			if err != nil {
				t.Fatalf("read script: %v", err)
			}
			content := string(raw)
			for _, want := range tc.want {
				if !strings.Contains(content, want) {
					t.Fatalf("%s missing %q", tc.path, want)
				}
			}
		})
	}
}
