//go:build wago_precompiled

package wago

import (
	"strings"
	"testing"
)

func TestPrecompiledRejectsSourceBeforeDecode(t *testing.T) {
	for _, source := range [][]byte{nil, {0, 97, 115, 109, 1, 0, 0, 0}, []byte("malformed wasm")} {
		for name, compile := range map[string]func() (*Compiled, error){
			"Compile":           func() (*Compiled, error) { return Compile(source) },
			"CompileWithConfig": func() (*Compiled, error) { return CompileWithConfig(nil, source) },
		} {
			t.Run(name, func(t *testing.T) {
				compiled, err := compile()
				if compiled != nil || err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
					t.Fatalf("compiled=%v, err=%v; want unavailable before decoding", compiled, err)
				}
			})
		}
	}
}
