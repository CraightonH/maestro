package orchestrator

import (
	"fmt"

	"github.com/tjohnson/maestro/internal/redact"
)

// sanitizeError redacts secrets from the error message. It intentionally uses
// %s (not %w) to strip the original error chain, preventing wrapped errors
// from leaking sensitive tokens through errors.Is/As inspection.
func sanitizeError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s", redact.String(err.Error()))
}

func sanitizeOutput(raw string) string {
	return redact.String(raw)
}
