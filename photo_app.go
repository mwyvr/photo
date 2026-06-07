package photo

import "context"

// Build metadata — set via -ldflags at build time.
var (
	Version string
	Commit  string
)

// ReportError may be replaced in main() to hook into an error reporting service.
var ReportError = func(ctx context.Context, err error, args ...interface{}) {}
