//go:build (darwin || linux) && tinygo

package main

import "runtime"

// TinyGo does not expose StackSys. Report an unavailable metric as JSON null.
func stackSystemBytes(_ *runtime.MemStats) any { return nil }
