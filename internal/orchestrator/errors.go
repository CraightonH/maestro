package orchestrator

import "errors"

var (
	ErrApprovalNotFound = errors.New("approval request not found")
	ErrMessageNotFound  = errors.New("message request not found")
	ErrRunNotFound      = errors.New("run not found")
)
