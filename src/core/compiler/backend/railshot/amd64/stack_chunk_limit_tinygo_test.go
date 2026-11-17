//go:build amd64 && tinygo

package amd64

import "testing"

func TestTinyGoStackChunkLimit(t *testing.T) {
	if maxStackChunkCap != 256 {
		t.Fatalf("TinyGo chunk limit = %d, want 256", maxStackChunkCap)
	}
}
