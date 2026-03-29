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
					BaseURL: "https://api.linear.app/graphql",
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
					BaseURL: "https://api.linear.app/graphql",
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
					BaseURL: "https://gitlab.example.com",
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
					BaseURL: "https://gitlab.example.com",
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
					BaseURL: "https://gitlab.example.com",
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
					BaseURL: "https://gitlab.example.com",
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
					BaseURL: "https://gitlab.example.com",
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
					BaseURL: "https://gitlab.example.com",
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
					BaseURL: "https://gitlab.example.com",
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
					BaseURL: "https://gitlab.example.com",
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

func hasWarningContaining(warnings []string, needle string) bool {
	for _, warning := range warnings {
		if strings.Contains(warning, needle) {
			return true
		}
	}
	return false
}
