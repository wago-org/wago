package wago

import "github.com/wago-org/wago/internal/wagopaths"

// Dirs is retained as a public alias while its implementation lives in a
// runtime-independent package used by the standard-Go version manager.
type Dirs = wagopaths.Dirs

func DirsFor(version string) Dirs { return wagopaths.DirsFor(version) }
