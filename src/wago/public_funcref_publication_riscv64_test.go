//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"fmt"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"testing"
)

func TestRV64PublicFuncrefTokenRequiresPublishedLocalRow(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, cached := range []bool{false, true} {
			for _, field := range []int{0, 8, 16, 24, 32} {
				t.Run(fmt.Sprintf("artifact%t/cached%t/field%d", artifact, cached, field), func(t *testing.T) {
					c := rv64TableStageCompile(t, "funcref-publication", rv64PublicReferenceFunctionBoundaryModule(), artifact)
					rt := NewRuntime()
					defer rt.Close()
					in := rv64NativeTokenInstantiate(t, rt, c, NewImports())
					defer in.Close()
					descriptor, ok := in.localFuncrefDescriptor(0)
					if !ok {
						t.Fatal("missing local descriptor")
					}
					token := uint64(0)
					var err error
					if cached {
						token, err = in.refStore.issue(in, descriptor)
						if err != nil || token == 0 {
							t.Fatal("valid token", token, err)
						}
					}
					roots := in.referenceLifetime().snapshot().ResourceRoots
					count := len(in.refStore.byToken)
					row := in.funcRefDescs[coreruntime.FuncRefDescBytes : 2*coreruntime.FuncRefDescBytes]
					word := binary.LittleEndian.Uint64(row[field:])
					binary.LittleEndian.PutUint64(row[field:], word^1)
					if issued, err := in.refStore.issue(in, descriptor); err == nil || issued != 0 {
						t.Fatal("corrupt publication issued token", issued, err)
					}
					if len(in.refStore.byToken) != count || in.referenceLifetime().snapshot().ResourceRoots != roots {
						t.Fatal("failed issuance changed token ownership")
					}
					binary.LittleEndian.PutUint64(row[field:], word)
					issued, err := in.refStore.issue(in, descriptor)
					if err != nil || issued == 0 || cached && issued != token {
						t.Fatal("restored publication", issued, token, err)
					}
				})
			}
		}
	}
}
