//go:build !tinygo

package wago

import "testing"

func requireStandardGoTestRuntime(*testing.T) bool { return true }
