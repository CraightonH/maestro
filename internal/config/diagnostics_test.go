package config_test

import (
	"strings"
	"testing"

	"github.com/tjohnson/maestro/internal/config"
)

func TestDiagnoseConfigWarnsOnIndistinguishableLinearRoutes(t *testing.T) {
	cfg := &config.Config{
		Defaults: testDefaults(1),
		Sources: []config.SourceConfig{
			{
				Name:    "triage-a",
				Tracker: "linear",
				Connection: config.SourceConnection{
					Domain:  "api.linear.app",
					Project: "maestro",
				},
				Filter: config.FilterConfig{
					Labels: []string{"agent:ready"},
					States: []string{"Todo"},
				},
			},
			{
				Name:    "triage-b",
				Tracker: "linear",
				Connection: config.SourceConnection{
					Domain:  "api.linear.app",
					Project: "maestro",
				},
				Filter: config.FilterConfig{
					Labels: []string{"agent:ready"},
					States: []string{"todo"},
				},
			},
		},
	}

	warnings := config.DiagnoseConfig(cfg)
	if !hasWarningContaining(warnings, `sources "triage-a" and "triage-b"`) {
		t.Fatalf("warnings = %v, want route-collision warning for triage-a/triage-b", warnings)
	}
	if !hasWarningContaining(warnings, "indistinguishable effective intake filters") {
		t.Fatalf("warnings = %v, want indistinguishable filter detail", warnings)
	}
}

func TestDiagnoseConfigWarnsWhenBroadSourceSubsumesNarrowerRoute(t *testing.T) {
	cfg := &config.Config{
		Defaults: testDefaults(1),
		Sources: []config.SourceConfig{
			{
				Name:        "all-ready",
				Tracker:     "gitlab",
				LabelPrefix: "orch",
				Connection: config.SourceConnection{
					Domain: "gitlab.example.com",
					Project: "team/project",
				},
				Filter: config.FilterConfig{
					Labels: []string{"agent:ready"},
					States: []string{"opened"},
				},
			},
			{
				Name:        "review-ready",
				Tracker:     "gitlab",
				LabelPrefix: "review",
				Connection: config.SourceConnection{
					Domain: "gitlab.example.com",
					Project: "team/project",
				},
				Filter: config.FilterConfig{
					Labels: []string{"agent:ready", "stage:review"},
					States: []string{"opened"},
				},
			},
		},
	}

	warnings := config.DiagnoseConfig(cfg)
	if !hasWarningContaining(warnings, `source "all-ready" subsumes "review-ready"`) {
		t.Fatalf("warnings = %v, want subsumption detail", warnings)
	}
	if !hasWarningContaining(warnings, `different lifecycle prefixes ("orch" vs "review")`) {
		t.Fatalf("warnings = %v, want prefix warning", warnings)
	}
}

func TestDiagnoseConfigWarnsOnGitLabProjectAndEpicOverlap(t *testing.T) {
	cfg := &config.Config{
		Defaults: testDefaults(1),
		Sources: []config.SourceConfig{
			{
				Name:    "project-ready",
				Tracker: "gitlab",
				Connection: config.SourceConnection{
					Domain: "gitlab.example.com",
					Project: "team/platform/repo",
				},
				Filter: config.FilterConfig{
					States: []string{"opened"},
				},
			},
			{
				Name:    "epic-ready",
				Tracker: "gitlab-epic",
				Connection: config.SourceConnection{
					Domain: "gitlab.example.com",
					Group:   "team/platform",
				},
				Filter: config.FilterConfig{
					Labels: []string{"bucket:ready"},
					States: []string{"opened"},
				},
				EpicFilter: config.FilterConfig{
					IIDs: []int{7},
				},
			},
		},
	}

	warnings := config.DiagnoseConfig(cfg)
	if !hasWarningContaining(warnings, `sources "epic-ready" and "project-ready"`) &&
		!hasWarningContaining(warnings, `sources "project-ready" and "epic-ready"`) {
		t.Fatalf("warnings = %v, want project/epic collision warning", warnings)
	}
	if !hasWarningContaining(warnings, "gitlab-epic and its linked-issue intake does not inherit filter.labels") {
		t.Fatalf("warnings = %v, want gitlab-epic effective filter note", warnings)
	}
}

func TestDiagnoseConfigWarnsWhenCustomOnCompleteLeavesMultipleStagesEligible(t *testing.T) {
	cfg := &config.Config{
		Defaults: testDefaults(1),
		Sources: []config.SourceConfig{
			{
				Name:        "coding",
				Tracker:     "gitlab",
				LabelPrefix: "orch",
				Connection: config.SourceConnection{
					Domain: "gitlab.example.com",
					Project: "team/project",
				},
				Filter: config.FilterConfig{
					Labels: []string{"orch:coding"},
					States: []string{"opened"},
				},
				OnComplete: &config.LifecycleTransition{
					AddLabels: []string{"orch:review"},
				},
			},
			{
				Name:        "review",
				Tracker:     "gitlab",
				LabelPrefix: "orch",
				Connection: config.SourceConnection{
					Domain: "gitlab.example.com",
					Project: "team/project",
				},
				Filter: config.FilterConfig{
					Labels: []string{"orch:review"},
					States: []string{"opened"},
				},
			},
		},
	}

	warnings := config.DiagnoseConfig(cfg)
	if !hasWarningContaining(warnings, `source "coding" on_complete may leave the same tracker item eligible for multiple sources (coding, review)`) {
		t.Fatalf("warnings = %v, want stage-collision warning", warnings)
	}
}

func TestDiagnoseConfigSkipsCleanlySeparatedPipelineStages(t *testing.T) {
	cfg := &config.Config{
		Defaults: testDefaults(1),
		Sources: []config.SourceConfig{
			{
				Name:        "coding",
				Tracker:     "gitlab",
				LabelPrefix: "orch",
				Connection: config.SourceConnection{
					Domain: "gitlab.example.com",
					Project: "team/project",
				},
				Filter: config.FilterConfig{
					Labels: []string{"orch:coding"},
					States: []string{"opened"},
				},
				OnComplete: &config.LifecycleTransition{
					AddLabels:    []string{"orch:review"},
					RemoveLabels: []string{"orch:coding"},
				},
			},
			{
				Name:        "review",
				Tracker:     "gitlab",
				LabelPrefix: "orch",
				Connection: config.SourceConnection{
					Domain: "gitlab.example.com",
					Project: "team/project",
				},
				Filter: config.FilterConfig{
					Labels: []string{"orch:review"},
					States: []string{"opened"},
				},
			},
		},
	}

	warnings := config.DiagnoseConfig(cfg)
	if hasWarningContaining(warnings, `source "coding" on_complete may leave the same tracker item eligible for multiple sources`) {
		t.Fatalf("warnings = %v, did not want stage-collision warning", warnings)
	}
}

func TestDiagnoseConfigWarnsOnRiskyStatelessDockerReuse(t *testing.T) {
	cfg := &config.Config{
		Defaults: testDefaults(1),
		AgentTypes: []config.AgentTypeConfig{{
			Name:      "ops-shell",
			Harness:   "codex",
			Workspace: "git-clone",
			Docker: &config.DockerConfig{
				Image:          "maestro-agent:latest",
				EnvPassthrough: []string{"OPENAI_API_KEY"},
				Network:        "bridge",
				Security: &config.DockerSecurityConfig{
					Preset:         config.DockerSecurityPresetCompat,
					ReadOnlyRootFS: boolPtr(false),
				},
				Reuse: &config.DockerReuseConfig{Mode: config.DockerReuseModeStateless},
			},
		}},
	}

	warnings := config.DiagnoseConfig(cfg)
	if !hasWarningContaining(warnings, `docker.reuse.mode=stateless with workspace="git-clone"`) {
		t.Fatalf("warnings = %v, want workspace warning", warnings)
	}
	if !hasWarningContaining(warnings, `docker.env_passthrough`) {
		t.Fatalf("warnings = %v, want env passthrough warning", warnings)
	}
	if !hasWarningContaining(warnings, `broad bridge networking`) {
		t.Fatalf("warnings = %v, want network warning", warnings)
	}
	if !hasWarningContaining(warnings, `permissive docker.security profile`) {
		t.Fatalf("warnings = %v, want security warning", warnings)
	}
}

func TestDiagnoseConfigWarnsWhenGlobalConcurrencyCapsSourcesAndAgents(t *testing.T) {
	cfg := &config.Config{
		Defaults: testDefaults(1),
		Sources: []config.SourceConfig{
			{
				Name:          "implement",
				Tracker:       "gitlab",
				AgentType:     "code",
				MaxActiveRuns: 3,
				Connection: config.SourceConnection{
					Domain: "gitlab.example.com",
					Project: "team/project",
				},
			},
		},
		AgentTypes: []config.AgentTypeConfig{
			{
				Name:           "code",
				Harness:        "codex",
				Workspace:      "git-clone",
				Prompt:         "prompt.md",
				ApprovalPolicy: "auto",
				MaxConcurrent:  4,
			},
		},
	}

	warnings := config.DiagnoseConfig(cfg)
	if !hasWarningContaining(warnings, `agent "code" max_concurrent=4 exceeds defaults.max_concurrent_global=1`) {
		t.Fatalf("warnings = %v, want global agent cap warning", warnings)
	}
	if !hasWarningContaining(warnings, `source "implement" max_active_runs=3 exceeds defaults.max_concurrent_global=1`) {
		t.Fatalf("warnings = %v, want global source cap warning", warnings)
	}
	if !hasWarningContaining(warnings, `source "implement" concurrency resolves to source=3, agent=4, global=1, effective=1`) {
		t.Fatalf("warnings = %v, want effective concurrency summary", warnings)
	}
}

func hasWarningContaining(warnings []string, needle string) bool {
	for _, warning := range warnings {
		if strings.Contains(warning, needle) {
			return true
		}
	}
	return false
}

func boolPtr(value bool) *bool {
	v := value
	return &v
}
