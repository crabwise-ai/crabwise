package adapter

import (
	"context"

	"github.com/crabwise-ai/crabwise/internal/audit"
)

// Adapter monitors an AI agent source and emits audit events.
type Adapter interface {
	Start(ctx context.Context, events chan<- *audit.AuditEvent) error
	Stop() error
	CanEnforce() bool
}
