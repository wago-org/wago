//go:build amd64 && !tinygo

package amd64

import "testing"

func TestHostStackChunkLimit(t *testing.T) {
	if maxStackChunkCap != 8192 {
		t.Fatalf("ordinary Go chunk limit = %d, want 8192", maxStackChunkCap)
	}
}
