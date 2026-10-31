package wago

import (
	"testing"
)

func TestLoadRejectsRetiredArtifact(t *testing.T) {
	// The retired native-code header must never enter an executable mapping.
	if compiled, err := Load([]byte{'W', 'A', 'G', 'O', 7, 0x22, 1, 0, 2, 0}); err == nil || compiled != nil {
		if compiled != nil {
			_ = compiled.Close()
		}
		t.Fatalf("Load retired artifact = %v, %v; want rejection", compiled, err)
	}
}
