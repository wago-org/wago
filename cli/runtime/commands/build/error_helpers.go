package build

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/wago-org/wago/cli/internal/ui"
)

// These cold-path wrappers keep formatting and error-list construction out of
// each platform validation call site. Build publication has many deliberately
// specific failure checks, so centralizing the machinery avoids multiplying its
// linked-code cost without changing diagnostics or allocating on success.
//
//go:noinline
func formatBuildError(format string, values ...any) error {
	return fmt.Errorf(format, values...)
}

// Typed variants keep interface-slice construction in one cold function rather
// than repeating it at the many validation call sites.
//
//go:noinline
func buildErrorS(format, value string) error {
	return fmt.Errorf(format, value)
}

//go:noinline
func buildErrorE(format string, err error) error {
	return fmt.Errorf(format, err)
}

//go:noinline
func buildErrorSE(format, value string, err error) error {
	return fmt.Errorf(format, value, err)
}

//go:noinline
func newBuildError(message string) error {
	return errors.New(message)
}

// Keep the command's many cold failure exits out of its successful build path.
// These wrappers retain ui's exact exit and diagnostic behavior.
//
//go:noinline
func fatalBuildError(err error) {
	ui.Fatal("build: %v", err)
}

//go:noinline
func usageBuildError(err error) {
	ui.Usage("build: %v", err)
}

// sameBuildPath centralizes path normalization used by the cold publication
// boundary checks, avoiding repeated linked copies of filepath's wrappers.
//
//go:noinline
func sameBuildPath(left, right string) bool {
	return filepath.Clean(left) == filepath.Clean(right)
}
