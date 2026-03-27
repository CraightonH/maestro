package workspace

import "testing"

func TestGitLabAuthArgsMatchesOnlyConfiguredHTTPSHost(t *testing.T) {
	args := gitLabAuthArgs("https://gitlab.example.com/team/project.git", "gitlab.example.com", "secret")
	if len(args) != 2 {
		t.Fatalf("auth args len = %d, want 2", len(args))
	}
	if args[0] != "-c" {
		t.Fatalf("auth args[0] = %q, want -c", args[0])
	}
}

func TestGitLabAuthArgsSkipsNonMatchingOrNonHTTPSRemotes(t *testing.T) {
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
			if args := gitLabAuthArgs(tc.repoURL, tc.host, tc.token); len(args) != 0 {
				t.Fatalf("auth args = %v, want none", args)
			}
		})
	}
}
