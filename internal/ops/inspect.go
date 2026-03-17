package ops

import (
	"os"
	"sort"
	"strings"
	"time"

	"github.com/tjohnson/maestro/internal/config"
	"github.com/tjohnson/maestro/internal/domain"
	"github.com/tjohnson/maestro/internal/prompt"
	"github.com/tjohnson/maestro/internal/state"
)

type ConfigSummary struct {
	ConfigPath          string                `json:"config_path"`
	AgentPacksDir       string                `json:"agent_packs_dir,omitempty"`
	UserName            string                `json:"user_name,omitempty"`
	GitLabUsername      string                `json:"gitlab_username,omitempty"`
	LinearUsername      string                `json:"linear_username,omitempty"`
	WorkspaceRoot       string                `json:"workspace_root"`
	StateDir            string                `json:"state_dir"`
	LogDir              string                `json:"log_dir"`
	LogMaxFiles         int                   `json:"log_max_files"`
	MaxConcurrentGlobal int                   `json:"max_concurrent_global"`
	StallTimeout        string                `json:"stall_timeout,omitempty"`
	DefaultPollInterval string                `json:"default_poll_interval,omitempty"`
	Sources             []ConfigSourceSummary `json:"sources"`
	Agents              []ConfigAgentSummary  `json:"agents"`
}

type ConfigSourceSummary struct {
	Name              string   `json:"name"`
	DisplayGroup      string   `json:"display_group,omitempty"`
	Tags              []string `json:"tags,omitempty"`
	Tracker           string   `json:"tracker"`
	AgentType         string   `json:"agent_type,omitempty"`
	BaseURL           string   `json:"base_url,omitempty"`
	Project           string   `json:"project,omitempty"`
	Group             string   `json:"group,omitempty"`
	Repo              string   `json:"repo,omitempty"`
	FilterLabels      []string `json:"filter_labels,omitempty"`
	FilterIIDs        []int    `json:"filter_iids,omitempty"`
	FilterStates      []string `json:"filter_states,omitempty"`
	Assignee          string   `json:"assignee,omitempty"`
	EpicFilterLabels  []string `json:"epic_filter_labels,omitempty"`
	EpicFilterIIDs    []int    `json:"epic_filter_iids,omitempty"`
	IssueFilterLabels []string `json:"issue_filter_labels,omitempty"`
	IssueFilterIIDs   []int    `json:"issue_filter_iids,omitempty"`
	IssueStates       []string `json:"issue_states,omitempty"`
	PollInterval      string   `json:"poll_interval"`
	TokenEnv          string   `json:"token_env,omitempty"`
}

type ConfigAgentSummary struct {
	Description    string              `json:"description,omitempty"`
	Name           string              `json:"name"`
	InstanceName   string              `json:"instance_name,omitempty"`
	AgentPack      string              `json:"agent_pack,omitempty"`
	PackPath       string              `json:"pack_path,omitempty"`
	Harness        string              `json:"harness"`
	Workspace      string              `json:"workspace"`
	ApprovalPolicy string              `json:"approval_policy"`
	MaxConcurrent  int                 `json:"max_concurrent"`
	Prompt         string              `json:"prompt"`
	PromptBody     string              `json:"prompt_body,omitempty"`
	SystemPrompt   string              `json:"system_prompt,omitempty"`
	ContextFiles   []string            `json:"context_files,omitempty"`
	ContextBodies  []ConfigFileSummary `json:"context_bodies,omitempty"`
	Tools          []string            `json:"tools,omitempty"`
	Skills         []string            `json:"skills,omitempty"`
	EnvKeys        []string            `json:"env_keys,omitempty"`
}

type ConfigFileSummary struct {
	Path    string `json:"path"`
	Content string `json:"content,omitempty"`
}

type StateSummary struct {
	SourceName        string                 `json:"source_name"`
	Health            string                 `json:"health"`
	Path              string                 `json:"path"`
	Version           int                    `json:"version"`
	FinishedCount     int                    `json:"finished_count"`
	DoneCount         int                    `json:"done_count"`
	FailedCount       int                    `json:"failed_count"`
	RetryCount        int                    `json:"retry_count"`
	PendingCount      int                    `json:"pending_approvals_count"`
	ApprovalHistCount int                    `json:"approval_history_count"`
	LastError         string                 `json:"last_error,omitempty"`
	ActiveRun         *state.PersistedRun    `json:"active_run,omitempty"`
	Finished          []StateTerminalSummary `json:"finished,omitempty"`
	Retries           []state.RetryEntry     `json:"retries,omitempty"`
	PendingApprovals  []StateApprovalSummary `json:"pending_approvals,omitempty"`
	ApprovalHistory   []StateDecisionSummary `json:"approval_history,omitempty"`
}

type StateTerminalSummary struct {
	IssueID    string    `json:"issue_id"`
	Identifier string    `json:"identifier"`
	Status     string    `json:"status"`
	Attempt    int       `json:"attempt"`
	FinishedAt time.Time `json:"finished_at"`
}

type StateApprovalSummary struct {
	RequestID       string    `json:"request_id"`
	RunID           string    `json:"run_id"`
	IssueIdentifier string    `json:"issue_identifier,omitempty"`
	AgentName       string    `json:"agent_name,omitempty"`
	ToolName        string    `json:"tool_name"`
	ApprovalPolicy  string    `json:"approval_policy,omitempty"`
	RequestedAt     time.Time `json:"requested_at"`
	Resolvable      bool      `json:"resolvable"`
}

type StateDecisionSummary struct {
	RequestID       string    `json:"request_id"`
	RunID           string    `json:"run_id"`
	IssueIdentifier string    `json:"issue_identifier,omitempty"`
	ToolName        string    `json:"tool_name,omitempty"`
	Decision        string    `json:"decision"`
	Outcome         string    `json:"outcome,omitempty"`
	DecidedAt       time.Time `json:"decided_at"`
}

func SummarizeConfig(cfg *config.Config) ConfigSummary {
	summary := ConfigSummary{
		ConfigPath:          cfg.ConfigPath,
		AgentPacksDir:       cfg.AgentPacksDir,
		UserName:            cfg.User.Name,
		GitLabUsername:      cfg.User.GitLabUsername,
		LinearUsername:      cfg.User.LinearUsername,
		WorkspaceRoot:       cfg.Workspace.Root,
		StateDir:            cfg.State.Dir,
		LogDir:              cfg.Logging.Dir,
		LogMaxFiles:         cfg.Logging.MaxFiles,
		MaxConcurrentGlobal: cfg.Defaults.MaxConcurrentGlobal,
		StallTimeout:        cfg.Defaults.StallTimeout.Duration.String(),
		DefaultPollInterval: cfg.Defaults.PollInterval.Duration.String(),
	}
	for _, source := range cfg.Sources {
		effectiveIssueFilter := source.EffectiveIssueFilter()
		effectiveEpicFilter := source.EffectiveEpicFilter()
		summary.Sources = append(summary.Sources, ConfigSourceSummary{
			Name:              source.Name,
			DisplayGroup:      source.DisplayGroup,
			Tags:              append([]string(nil), source.Tags...),
			Tracker:           source.Tracker,
			AgentType:         source.AgentType,
			BaseURL:           source.Connection.BaseURL,
			Project:           source.Connection.Project,
			Group:             source.Connection.GroupPath(),
			Repo:              source.Repo,
			FilterLabels:      append([]string(nil), source.Filter.Labels...),
			FilterIIDs:        append([]int(nil), source.Filter.IIDs...),
			FilterStates:      append([]string(nil), source.Filter.States...),
			Assignee:          source.Filter.Assignee,
			EpicFilterLabels:  append([]string(nil), effectiveEpicFilter.Labels...),
			EpicFilterIIDs:    append([]int(nil), effectiveEpicFilter.IIDs...),
			IssueFilterLabels: append([]string(nil), effectiveIssueFilter.Labels...),
			IssueFilterIIDs:   append([]int(nil), effectiveIssueFilter.IIDs...),
			IssueStates:       append([]string(nil), effectiveIssueFilter.States...),
			PollInterval:      source.PollInterval.Duration.String(),
			TokenEnv:          source.Connection.TokenEnv,
		})
	}
	for _, agent := range cfg.AgentTypes {
		envKeys := make([]string, 0, len(agent.Env))
		for key := range agent.Env {
			envKeys = append(envKeys, key)
		}
		sort.Strings(envKeys)

		contextBodies := make([]ConfigFileSummary, 0, len(agent.ContextFiles))
		for _, path := range agent.ContextFiles {
			contextBodies = append(contextBodies, ConfigFileSummary{
				Path:    path,
				Content: readConfigText(path),
			})
		}
		summary.Agents = append(summary.Agents, ConfigAgentSummary{
			Description:    agent.Description,
			Name:           agent.Name,
			InstanceName:   agent.InstanceName,
			AgentPack:      agent.AgentPack,
			PackPath:       agent.PackPath,
			Harness:        agent.Harness,
			Workspace:      agent.Workspace,
			ApprovalPolicy: agent.ApprovalPolicy,
			MaxConcurrent:  agent.MaxConcurrent,
			Prompt:         agent.Prompt,
			PromptBody:     readConfigText(agent.Prompt),
			SystemPrompt:   prompt.SystemPreamble(),
			ContextFiles:   append([]string(nil), agent.ContextFiles...),
			ContextBodies:  contextBodies,
			Tools:          append([]string(nil), agent.Tools...),
			Skills:         append([]string(nil), agent.Skills...),
			EnvKeys:        envKeys,
		})
	}
	return summary
}

func SummarizeState(sourceName string, path string, snapshot state.Snapshot) StateSummary {
	summary := StateSummary{
		SourceName:        sourceName,
		Path:              path,
		Version:           snapshot.Version,
		FinishedCount:     len(snapshot.Finished),
		RetryCount:        len(snapshot.RetryQueue),
		PendingCount:      len(snapshot.PendingApprovals),
		ApprovalHistCount: len(snapshot.ApprovalHistory),
		ActiveRun:         snapshot.ActiveRun,
	}

	for _, finished := range snapshot.Finished {
		switch finished.Status {
		case domain.RunStatusDone:
			summary.DoneCount++
		case domain.RunStatusFailed:
			summary.FailedCount++
		}
		summary.Finished = append(summary.Finished, StateTerminalSummary{
			IssueID:    finished.IssueID,
			Identifier: finished.Identifier,
			Status:     string(finished.Status),
			Attempt:    finished.Attempt,
			FinishedAt: finished.FinishedAt,
		})
	}
	for _, retry := range snapshot.RetryQueue {
		summary.Retries = append(summary.Retries, retry)
	}
	for _, approval := range snapshot.PendingApprovals {
		summary.PendingApprovals = append(summary.PendingApprovals, StateApprovalSummary{
			RequestID:       approval.RequestID,
			RunID:           approval.RunID,
			IssueIdentifier: approval.IssueIdentifier,
			AgentName:       approval.AgentName,
			ToolName:        approval.ToolName,
			ApprovalPolicy:  approval.ApprovalPolicy,
			RequestedAt:     approval.RequestedAt,
			Resolvable:      approval.Resolvable,
		})
	}
	for _, decision := range snapshot.ApprovalHistory {
		summary.ApprovalHistory = append(summary.ApprovalHistory, StateDecisionSummary{
			RequestID:       decision.RequestID,
			RunID:           decision.RunID,
			IssueIdentifier: decision.IssueIdentifier,
			ToolName:        decision.ToolName,
			Decision:        decision.Decision,
			Outcome:         decision.Outcome,
			DecidedAt:       decision.DecidedAt,
		})
	}
	summary.LastError = summarizeStateLastError(snapshot)
	summary.Health = summarizeStateHealth(summary)
	return summary
}

func summarizeStateLastError(snapshot state.Snapshot) string {
	var latestTime time.Time
	lastError := ""
	for _, retry := range snapshot.RetryQueue {
		if strings.TrimSpace(retry.Error) == "" {
			continue
		}
		if latestTime.IsZero() || retry.DueAt.After(latestTime) {
			latestTime = retry.DueAt
			lastError = retry.Error
		}
	}
	for _, finished := range snapshot.Finished {
		if strings.TrimSpace(finished.Error) == "" {
			continue
		}
		if latestTime.IsZero() || finished.FinishedAt.After(latestTime) {
			latestTime = finished.FinishedAt
			lastError = finished.Error
		}
	}
	return lastError
}

func summarizeStateHealth(summary StateSummary) string {
	switch {
	case summary.RetryCount > 0:
		return "retrying"
	case summary.ActiveRun != nil && summary.FailedCount > 0:
		return "active+degraded"
	case summary.ActiveRun != nil:
		return "active"
	case summary.PendingCount > 0 && summary.FailedCount > 0:
		return "awaiting-approval+degraded"
	case summary.PendingCount > 0:
		return "awaiting-approval"
	case summary.FailedCount > 0:
		return "degraded"
	case summary.FinishedCount > 0:
		return "idle"
	default:
		return "empty"
	}
}

func readConfigText(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}
