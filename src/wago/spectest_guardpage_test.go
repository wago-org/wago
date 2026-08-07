//go:build (linux || darwin) && (amd64 || arm64) && wago_guardpage && !tinygo

package wago_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago/tests/wasmtest"
)

func TestSpecExecRetiresUnreferencedCurrentGuardMemory(t *testing.T) {
	t.Setenv("WAGO_BOUNDS", "signals")
	tmp := t.TempDir()
	const filename = "memory.wasm"
	module := wasmtest.Module(
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
	)
	if err := os.WriteFile(filepath.Join(tmp, filename), module, 0o600); err != nil {
		t.Fatal(err)
	}

	const modules = 300 // Deliberately exceeds the bounded 256-entry guard registry.
	commands := make([]specExecCmd, modules)
	for i := range commands {
		commands[i] = specExecCmd{Type: "module", Line: i + 1, Filename: filename}
	}
	stats := runSpecExecFile(t, "guard-retire", tmp, specExecFile{Commands: commands})
	if stats.modulesPassed != modules || stats.modulesFailed != 0 || stats.modulesSkipped != 0 {
		t.Fatalf("modules pass/fail/skip = %d/%d/%d, want %d/0/0", stats.modulesPassed, stats.modulesFailed, stats.modulesSkipped, modules)
	}
}
