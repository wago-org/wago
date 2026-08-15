//go:build darwin && !wago_lean

package run

import (
	"strings"
	"testing"
)

func TestDarwinRejectsWatch(t *testing.T) {
	_, err := Command(testEnvironment{}).Parse("wago run", []string{"--watch", "module.wasm"})
	if err == nil || !strings.Contains(err.Error(), "unknown flag --watch") {
		t.Fatalf("Darwin --watch error = %v", err)
	}
}
