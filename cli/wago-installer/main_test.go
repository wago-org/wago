package main

import (
	"runtime/debug"
	"testing"
)

func TestResolveInstallerVersion(t *testing.T) {
	tests := []struct {
		name    string
		stamped string
		info    *debug.BuildInfo
		want    string
	}{
		{name: "release build", stamped: "v1.2.3", info: &debug.BuildInfo{Main: debug.Module{Version: "v1.2.2"}}, want: "v1.2.3"},
		{name: "go install", info: &debug.BuildInfo{Main: debug.Module{Version: "v1.2.3"}}, want: "v1.2.3"},
		{name: "local build", info: &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}},
		{name: "no build info"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := resolveInstallerVersion(test.stamped, test.info); got != test.want {
				t.Fatalf("resolveInstallerVersion(%q) = %q, want %q", test.stamped, got, test.want)
			}
		})
	}
}
