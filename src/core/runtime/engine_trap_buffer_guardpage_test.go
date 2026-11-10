//go:build wago_guardpage

package runtime

import (
	"bytes"
	"strings"
	"testing"
)

func TestCallGuardedRejectsIncompleteTrapBuffersBeforeNativeEntry(t *testing.T) {
	jm, err := NewJobMemoryGuarded(0, wasmPageBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer jm.Close()

	engine := &Engine{}
	for length := 0; length < TrapBufferBytes; length++ {
		backing := bytes.Repeat([]byte{0xaa}, TrapBufferBytes)
		err := engine.CallGuarded(0, nil, jm.LinMemBase(), backing[:length], nil, jm)
		if err == nil || !strings.Contains(err.Error(), "trap buffer") {
			t.Fatalf("trap length %d error = %v", length, err)
		}
		if want := bytes.Repeat([]byte{0xaa}, TrapBufferBytes); !bytes.Equal(backing, want) {
			t.Fatalf("trap length %d changed backing bytes: %x", length, backing)
		}
	}
}
