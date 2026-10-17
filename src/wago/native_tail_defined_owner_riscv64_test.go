//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"errors"
	"fmt"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func rv64DefinedTailOwnerForTest(in *Instance) (*railshot.NativeTailDefinedOwner, error) {
	nativeExecutionMu.Lock()
	defer nativeExecutionMu.Unlock()
	return rv64NativeDefinedTailOwner(in)
}

func TestRV64PrivateTailDefinedOwnerSnapshots(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			for _, shared := range []bool{false, true} {
				t.Run(fmt.Sprintf("vector%t-artifact%t-shared%t", vector, artifact, shared), func(t *testing.T) {
					root, producer, table, c := rv64TableTargetFixture(t, vector, artifact, shared)
					if _, err := rv64DefinedTailOwnerForTest(producer); !errors.Is(err, railshot.ErrUnsupported) {
						t.Fatal("definition snapshot without a physical lease", err)
					}
					if !table.retainProducerInstance(producer) {
						t.Fatal("table producer root")
					}
					var targets rv64NativeTableTargets
					defer targets.release()
					if err := rv64TableTargetRefresh(&targets, root); err != nil {
						t.Fatal(err)
					}
					first, err := rv64DefinedTailOwnerForTest(root)
					if err != nil {
						t.Fatal("root defined owner", err)
					}
					second, err := rv64DefinedTailOwnerForTest(producer)
					if err != nil || first == second {
						t.Fatal("actual producer identity", err)
					}
					if n := testing.AllocsPerRun(4, func() {
						owner, err := rv64DefinedTailOwnerForTest(producer)
						if err != nil || owner != second {
							panic("defined owner refresh")
						}
					}); n != 0 {
						t.Fatal("warm runtime defined-owner allocations", n)
					}
					// The cached snapshot still authenticates live canonical fields.
					row := producer.funcRefDescs[80:120]
					key := binary.LittleEndian.Uint64(row[8:])
					binary.LittleEndian.PutUint64(row[8:], key^8)
					if _, err := rv64DefinedTailOwnerForTest(producer); !errors.Is(err, railshot.ErrUnsupported) {
						t.Fatal("changed canonical signature row", err)
					}
					binary.LittleEndian.PutUint64(row[8:], key)
					if err := c.Close(); err != nil {
						t.Fatal(err)
					}
					if err := producer.Close(); err != nil {
						t.Fatal(err)
					}
					if _, err := root.Invoke("clear", 0); err != nil {
						t.Fatal(err)
					}
					if producer.resourcesClosed || second.Validate() != nil {
						t.Fatal("suspended owner lost its frozen physical snapshot")
					}
					targets.release()
					// Stale metadata must reject its expired image lease before reading
					// the context/descriptor arena which has now been unmapped.
					if !producer.resourcesClosed || !errors.Is(second.Validate(), railshot.ErrUnsupported) {
						t.Fatal("physically retired defined-owner snapshot reused")
					}
					if err := rv64TableTargetRefresh(&targets, root); err != nil {
						t.Fatal(err)
					}
					if again, err := rv64DefinedTailOwnerForTest(root); err != nil || again != first {
						t.Fatal("cached metadata under a fresh physical lease", err)
					}
				})
			}
		}
	}
}

func TestRV64PrivateTailDefinedOwnerRejectsProxyRows(t *testing.T) {
	c := rv64NativeTailCompile(t, "c2s-proxy-owner-gate", rv64TableFunctionImportModule([]wasm.ValType{wasm.I32}, false, false, nil, false, true), false)
	in, err := Instantiate(c, NewImports().Function("env", "host", HostCallFunc(func(HostCall) {})))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if !in.retainResourceRoot() {
		t.Fatal("physical root")
	}
	defer in.releaseResourceRoot()
	if owner, err := rv64DefinedTailOwnerForTest(in); owner != nil || !errors.Is(err, railshot.ErrUnsupported) {
		t.Fatal("ordinary import proxy treated as a defined native entry", err)
	}
}
