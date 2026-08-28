package runtime

import (
	"strings"
	"testing"
)

func TestEngineRejectsShortTrapCellsBeforeNativeEntry(t *testing.T) {
	engine := &Engine{}
	for length := 0; length < 4; length++ {
		backing := []byte{0xaa, 0xbb, 0xcc, 0xdd}
		trap := backing[:length]
		for name, call := range map[string]func() error{
			"Call":         func() error { return engine.Call(0, nil, nil, trap, nil) },
			"CallPrepared": func() error { return engine.CallPrepared(0, nil, 0, trap, nil) },
		} {
			err := call()
			if err == nil || !strings.Contains(err.Error(), "trap cell") {
				t.Fatalf("%s trap length %d error = %v", name, length, err)
			}
			if got := backing; got[0] != 0xaa || got[1] != 0xbb || got[2] != 0xcc || got[3] != 0xdd {
				t.Fatalf("%s trap length %d changed backing bytes: %x", name, length, got)
			}
		}
	}
}
