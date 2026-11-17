//go:build compiler_profile

package main

import (
	"os"
	"runtime/pprof"
)

func startProfile(path string) func() {
	if path == "" {
		return func() {}
	}
	f, err := os.Create(path)
	check(err)
	check(pprof.StartCPUProfile(f))
	return func() { pprof.StopCPUProfile(); check(f.Close()) }
}
