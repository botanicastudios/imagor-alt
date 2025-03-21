// Package processor provides interfaces for image processors
package processor

import (
	"context"
)

// Blob represents an image blob
type Blob struct {
	Data []byte
}

// Processor interface for image processors
type Processor interface {
	Process(ctx context.Context, blob *Blob, args string) (*Blob, error)
	Startup(ctx context.Context) error
	Shutdown(ctx context.Context) error
}

// Prefilter interface for prefilter implementations
type Prefilter interface {
	Name() string
	Apply(ctx context.Context, blob *Blob, args string) (*Blob, error)
}

// IsPrefilter checks if a filter is a prefilter
func IsPrefilter(filterName string) bool {
	prefilters := map[string]bool{
		"depthmap": true,
		"removebg": true,
	}
	return prefilters[filterName]
}
