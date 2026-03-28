package orchestrator

import (
	"context"
	"time"
)

var harnessControlTimeout = 15 * time.Second
var harnessShutdownTimeout = 5 * time.Second

func withHarnessControlTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), harnessControlTimeout)
}

func withHarnessShutdownTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), harnessShutdownTimeout)
}
