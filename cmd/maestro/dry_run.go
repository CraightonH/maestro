package main

import (
	"fmt"
	"strings"

	"github.com/tjohnson/maestro/internal/config"
	"github.com/tjohnson/maestro/internal/orchestrator"
)

const (
	dryRunPromptPreviewLines = 18
	dryRunPromptPreviewChars = 1600
)

func formatDryRunReport(cfg *config.Config, report orchestrator.DryRunReport) string {
	var b strings.Builder
	b.WriteString("Maestro dry run\n")
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("Config: %s\n", cfg.ConfigPath))
	b.WriteString(fmt.Sprintf("Sources: %d\n", len(report.Sources)))

	for _, source := range report.Sources {
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("Source: %s\n", source.Name))
		b.WriteString(fmt.Sprintf("Tracker: %s\n", source.Tracker))
		b.WriteString(fmt.Sprintf("Polled issues: %d\n", source.PolledCount))
		if len(source.StateWarnings) > 0 {
			for _, warning := range source.StateWarnings {
				b.WriteString(fmt.Sprintf("Warning: %s\n", warning))
			}
		}
		if source.MatchedIssue {
			b.WriteString(fmt.Sprintf("Matching issue: yes (%s)\n", source.IssueIdentifier))
			if source.IssueTitle != "" {
				b.WriteString(fmt.Sprintf("Title: %s\n", source.IssueTitle))
			}
			if source.IssueState != "" {
				b.WriteString(fmt.Sprintf("State: %s\n", source.IssueState))
			}
			b.WriteString(fmt.Sprintf("Eligibility: %s\n", source.Reason))
			b.WriteString(fmt.Sprintf("Candidate source: %s\n", source.CandidateSource))
			b.WriteString(fmt.Sprintf("Agent: %s (%s)\n", source.AgentType, source.AgentName))
			b.WriteString(fmt.Sprintf("Attempt: %d\n", source.Attempt))
			b.WriteString(fmt.Sprintf("Workspace: %s\n", source.WorkspaceStrategy))
			b.WriteString(fmt.Sprintf("Workspace path: %s\n", source.WorkspacePath))
			if source.WorkspaceBranch != "" {
				b.WriteString(fmt.Sprintf("Workspace branch: %s\n", source.WorkspaceBranch))
			}
			b.WriteString("Dispatch lifecycle:\n")
			b.WriteString(fmt.Sprintf("  Add labels: %s\n", formatList(source.Lifecycle.AddLabels)))
			b.WriteString(fmt.Sprintf("  Remove labels: %s\n", formatList(source.Lifecycle.RemoveLabels)))
			if strings.TrimSpace(source.Lifecycle.State) == "" {
				b.WriteString("  State: none\n")
			} else {
				b.WriteString(fmt.Sprintf("  State: %s\n", source.Lifecycle.State))
			}
			b.WriteString("Prompt preview:\n")
			if strings.TrimSpace(source.PromptPreview) == "" {
				b.WriteString("  unavailable\n")
			} else {
				for _, line := range strings.Split(clipPromptPreview(source.PromptPreview), "\n") {
					b.WriteString(fmt.Sprintf("  %s\n", line))
				}
			}
			if strings.TrimSpace(source.PromptWarning) != "" {
				b.WriteString(fmt.Sprintf("Prompt note: %s\n", source.PromptWarning))
			}
		} else {
			b.WriteString("Matching issue: no\n")
			b.WriteString(fmt.Sprintf("Eligibility: %s\n", source.Reason))
		}
		if len(source.Evaluations) > 0 {
			b.WriteString("Evaluations:\n")
			for _, evaluation := range source.Evaluations {
				b.WriteString(fmt.Sprintf("  [%s] %s: %s (%s)\n", evaluation.Stage, evaluation.Identifier, evaluation.Outcome, evaluation.Reason))
			}
		}
	}

	return b.String()
}

func formatList(items []string) string {
	if len(items) == 0 {
		return "none"
	}
	return strings.Join(items, ", ")
}

func clipPromptPreview(raw string) string {
	raw = strings.TrimRight(raw, "\n")
	lines := strings.Split(raw, "\n")
	clipped := false
	if len(lines) > dryRunPromptPreviewLines {
		lines = lines[:dryRunPromptPreviewLines]
		clipped = true
	}
	text := strings.Join(lines, "\n")
	runes := []rune(text)
	if len(runes) > dryRunPromptPreviewChars {
		text = string(runes[:dryRunPromptPreviewChars])
		clipped = true
	}
	if clipped {
		return strings.TrimRight(text, "\n") + "\n...[clipped]"
	}
	return text
}
