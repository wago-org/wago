//go:build wago_lean

package run

import (
	"strings"
	"testing"
)

func TestLeanProfileRejectsWatch(t *testing.T) {
	_, err := Command(testEnvironment{}).Parse("wago run", []string{"--watch", "module.wasm"})
	if err == nil || !strings.Contains(err.Error(), "unknown flag --watch") {
		t.Fatalf("lean --watch error = %v", err)
	}
}
