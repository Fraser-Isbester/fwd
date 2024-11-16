package processor

import (
	"context"

	"github.com/fraser-isbester/fwd/pkg/types"
)

// DataSource represents any source of events (collectors or receivers)
type DataSource interface {
	Start(context.Context, chan<- *types.Event) error
	Stop() error
	Name() string
}

// Processor is the main interface for event processing systems
type Processor interface {
	// RegisterSource adds a new data source to the processor
	RegisterSource(DataSource)

	// Start begins processing events from all registered sources
	Start(context.Context) error

	// Stop gracefully shuts down the processor and all sources
	Stop() error

	// Name returns the processor's identifier
	Name() string
}
