//go:build ((linux && (amd64 || arm64)) || (darwin && arm64) || (linux && riscv64 && wago_rv64_experimental)) && !tinygo && !wago_target_tinygo

package wago

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestImportedStartPreservesHostTrapCauses(t *testing.T) {
	source := wasmtest.Module(wasmtest.Section(1, []byte{1, 0x60, 0, 0}), wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("start")...), 0, 0))), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(8, []byte{0}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}))))
	c, err := Compile(nil, source)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	blob, err := c.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadTrustedArtifact(blob)
	if err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	cause := errors.New("imported start sentinel")
	for index, compiled := range []*Compiled{c, loaded} {
		for _, tc := range []struct {
			name  string
			value any
			want  error
		}{{"value", HostTrap{Err: cause}, cause}, {"pointer", &HostTrap{Err: cause}, cause}, {"nil-error", HostTrap{}, nil}, {"nil-pointer", (*HostTrap)(nil), nil}} {
			t.Run(fmt.Sprintf("artifact%d-%s", index, tc.name), func(t *testing.T) {
				in, err := Instantiate(compiled, NewImports().Function("env", "start", HostCallFunc(func(HostCall) { panic(tc.value) })))
				if in != nil {
					defer in.Close()
					t.Fatal("trapped imported start returned instance")
				}
				if tc.want != nil {
					if !errors.Is(err, tc.want) {
						t.Fatal("host trap cause lost", err)
					}
				} else if err == nil || !strings.Contains(err.Error(), "host trapped without an error") {
					t.Fatal("empty host trap accepted", err)
				}
			})
		}
	}
}
