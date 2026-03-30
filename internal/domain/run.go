package domain

import "time"

type RunStatus string

const (
	RunStatusPending   RunStatus = "pending"
	RunStatusPreparing RunStatus = "preparing"
	RunStatusActive    RunStatus = "active"
	RunStatusAwaiting  RunStatus = "awaiting_approval"
	RunStatusDone      RunStatus = "done"
	RunStatusFailed    RunStatus = "failed"
)

type ApprovalState string

const (
	ApprovalStateApproved ApprovalState = "approved"
	ApprovalStateAwaiting ApprovalState = "awaiting"
	ApprovalStateRejected ApprovalState = "rejected"
)

type ContainerReuseMetadata struct {
	Mode          string
	Reused        bool
	ContainerID   string
	ContainerName string
	ProfileKey    string
	LineageKey    string
}

type RunExecutionMetadata struct {
	Mode           string
	ContainerReuse *ContainerReuseMetadata
}

// AgentRun tracks the lifecycle of a single agent execution.
type AgentRun struct {
	ID             string
	AgentName      string
	AgentType      string
	Issue          Issue
	SourceName     string
	HarnessKind    string
	WorkspacePath  string
	Status         RunStatus
	Attempt        int
	ApprovalPolicy string
	ApprovalState  ApprovalState
	StartedAt      time.Time
	LastActivityAt time.Time
	CompletedAt    time.Time
	Execution      *RunExecutionMetadata
	Metrics        RunMetrics
	Error          string
}
