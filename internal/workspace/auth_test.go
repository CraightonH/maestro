package workspace

import "testing"

func TestGitLabAuthEnvMatchesOnlyConfiguredHTTPSHost(t *testing.T) {
	env := gitLabAuthEnv("https://gitlab.example.com/team/project.git", "gitlab.example.com", "secret")
	if len(env) != 3 {
		t.Fatalf("auth env len = %d, want 3", len(env))
	}
	if env[0] != "GIT_CONFIG_COUNT=1" {
		t.Fatalf("auth env[0] = %q, want GIT_CONFIG_COUNT=1", env[0])
	}
	if env[1] != "GIT_CONFIG_KEY_0=http.extraHeader" {
		t.Fatalf("auth env[1] = %q, want http.extraHeader key", env[1])
	}
	if env[2] == "" || env[2] == "GIT_CONFIG_VALUE_0=" {
		t.Fatalf("auth env[2] = %q, want auth header value", env[2])
	}
}

func TestGitLabAuthEnvSkipsNonMatchingOrNonHTTPSRemotes(t *testing.T) {
	cases := []struct {
		name    string
		repoURL string
		host    string
		token   string
	}{
		{name: "blank token", repoURL: "https://gitlab.example.com/team/project.git", host: "gitlab.example.com", token: ""},
		{name: "host mismatch", repoURL: "https://github.com/team/project.git", host: "gitlab.example.com", token: "secret"},
		{name: "scp ssh", repoURL: "git@gitlab.example.com:team/project.git", host: "gitlab.example.com", token: "secret"},
		{name: "ssh url", repoURL: "ssh://git@gitlab.example.com/team/project.git", host: "gitlab.example.com", token: "secret"},
		{name: "http scheme", repoURL: "http://gitlab.example.com/team/project.git", host: "gitlab.example.com", token: "secret"},
		{name: "garbage", repoURL: "::not-a-url::", host: "gitlab.example.com", token: "secret"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if env := gitLabAuthEnv(tc.repoURL, tc.host, tc.token); len(env) != 0 {
				t.Fatalf("auth env = %v, want none", env)
			}
		})
	}
}
