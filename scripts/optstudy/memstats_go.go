//go:build (darwin || linux) && !tinygo

package main

import "runtime"

func stackSystemBytes(m *runtime.MemStats) any { return m.StackSys }
