package runtime

import (
	"bytes"
	"strings"
	"testing"
)

func TestEngineRejectsIncompleteTrapBuffersBeforeNativeEntry(t *testing.T) {
	engine := &Engine{}
	for length := 0; length < TrapBufferBytes; length++ {
		backing := bytes.Repeat([]byte{0xaa}, TrapBufferBytes)
		trap := backing[:length]
		for name, call := range map[string]func() error{
			"Call":         func() error { return engine.Call(0, nil, nil, trap, nil) },
			"CallPrepared": func() error { return engine.CallPrepared(0, nil, 0, trap, nil) },
		} {
			err := call()
			if err == nil || !strings.Contains(err.Error(), "trap buffer") {
				t.Fatalf("%s trap length %d error = %v", name, length, err)
			}
			if want := bytes.Repeat([]byte{0xaa}, TrapBufferBytes); !bytes.Equal(backing, want) {
				t.Fatalf("%s trap length %d changed backing bytes: %x", name, length, backing)
			}
		}
	}
}

func TestJobMemoryRejectsIncompleteTrapBuffers(t *testing.T) {
	jm, err := NewJobMemory(0)
	if err != nil {
		t.Fatal(err)
	}
	defer jm.Close()

	for length := 0; length < TrapBufferBytes; length++ {
		for name, bind := range map[string]func([]byte) error{
			"BindTrapCell":   jm.BindTrapCell,
			"RebindTrapCell": jm.RebindTrapCell,
		} {
			backing := bytes.Repeat([]byte{0xaa}, TrapBufferBytes)
			err := bind(backing[:length])
			if err == nil || !strings.Contains(err.Error(), "trap buffer") {
				t.Fatalf("%s trap length %d error = %v", name, length, err)
			}
			if want := bytes.Repeat([]byte{0xaa}, TrapBufferBytes); !bytes.Equal(backing, want) {
				t.Fatalf("%s trap length %d changed backing bytes: %x", name, length, backing)
			}
		}
	}
}
