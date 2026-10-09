//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

func TestReferenceFunctionModuleRV64ClosedDescriptorTransport(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, tails := range []bool{false, true} {
			for _, indexed := range []bool{false, true} {
				for _, nullable := range []bool{false, true} {
					t.Run(fmt.Sprintf("vector%t-tails%t-indexed%t-nullable%t", vector, tails, indexed, nullable), func(t *testing.T) {
						m := closedReferenceDecode(t, vector, tails, indexed, nullable)
						if nullable {
							body := m.Code[0].BodyBytes
							at := bytes.Index(body, []byte{0x20, 0, 0x20, 1, 0x10, 2})
							if at < 0 {
								t.Fatal("root call boundary")
							}
							heap := byte(0x70)
							if indexed {
								heap = 0
							}
							prefix := []byte{0x20, 0, 0x41, 2, 0x71, 4, 0x63, heap, 0xd0, heap, 5}
							prefix = append(prefix, body[:at]...)
							prefix = append(prefix, 0x0b)
							m.Code[0].BodyBytes = append(prefix, body[at:]...)
						}
						c, err := closedReferenceCompile(m, vector, tails)
						if err != nil {
							t.Fatal("whole-module code", err)
						}
						base := thunkImageRV64(t, c.code)
						e, j, args, results, trap, _ := thunkFixtureRV64(t, 256)
						j.SetRV64InvocationView(e.RV64InvocationViewPointer())
						t.Cleanup(func() { j.SetRV64InvocationView(0) })
						arena, err := coreruntime.NewArena(6*coreruntime.FuncRefDescBytes + 16)
						if err != nil {
							t.Fatal(err)
						}
						t.Cleanup(func() {
							j.SetFuncRefDesc(0)
							if err := arena.Close(); err != nil {
								t.Error(err)
							}
						})
						// These private descriptor rows point at actual retained numeric code and
						// JobMemory. They exercise identity transport only, not public owner tokens.
						dir := arena.Alloc(6*coreruntime.FuncRefDescBytes + 16)
						for i := range dir {
							dir[i] = 0xa5
						}
						home, ok := abi.TagFuncRefHome(uint64(j.LinMemBase()), abi.FuncRefEntryLocalWrapper)
						if !ok {
							t.Fatal("home tag")
						}
						for _, idx := range []int{1, 4} {
							off := (idx + 1) * coreruntime.FuncRefDescBytes
							row := dir[off : off+coreruntime.FuncRefDescBytes]
							binary.LittleEndian.PutUint64(row[coreruntime.TableEntryCodePtrOffset:], uint64(base)+uint64(c.functions[idx].Entry))
							binary.LittleEndian.PutUint64(row[coreruntime.TableEntrySigKeyOffset:], wasm.StructuralFuncTypeKey(&m.Types[0].SubTypes[0].Comp))
							binary.LittleEndian.PutUint64(row[coreruntime.TableEntryHomeLinMemOffset:], home)
							binary.LittleEndian.PutUint64(row[coreruntime.TableEntryRefSlotOffset:], uint64(thunkPointerRV64(row)))
							binary.LittleEndian.PutUint64(row[coreruntime.FuncRefContextOffset:], 0) // unused private context: no descriptor call is admitted
						}
						before := bytes.Clone(dir)
						j.SetFuncRefDesc(thunkPointerRV64(dir))

						write := func(n uint64) {
							for i := range args {
								args[i] = 0x5a
							}
							for i := range results {
								results[i] = 0xa5
							}
							clear(trap)
							binary.LittleEndian.PutUint64(args, n)
							binary.LittleEndian.PutUint64(args[8:], 0x7ff0000000000001)
							if vector {
								binary.LittleEndian.PutUint64(args[16:], 0x8000000000000000)
							}
						}
						for _, n := range []uint64{0xdeadbeef00000000, 0xffffffff00000001, 0x8000000000000002, 0x12345678ffffffff, 0x80000000} {
							write(n)
							if err := e.CallPrepared(base+uintptr(c.functions[0].Entry), args, j.LinMemBase(), trap, results); err != nil {
								t.Fatal("closed numeric entry", err)
							}
							idx := 1
							if n&1 == 0 {
								idx = 4
							}
							wantRef := uint64(thunkPointerRV64(dir[(idx+1)*coreruntime.FuncRefDescBytes:]))
							null := uint64(0)
							if nullable && n&2 != 0 {
								wantRef = 0
								null = 1
							}
							// The real shared argument arena contains the tuple that arrived at the
							// last reference-valued callee, including the full descriptor identity.
							if got := binary.LittleEndian.Uint64(args); got != wantRef {
								t.Fatal("descriptor index holes/full reference", idx, got, wantRef)
							}
							want := []uint64{0x7ff0000000000001}
							if vector {
								want = append(want, 0x8000000000000000)
							}
							want = append(want, uint64(uint32(n)+5), null)
							for i, v := range want {
								if got := binary.LittleEndian.Uint64(results[8*i:]); got != v {
									t.Fatal("numeric result from arriving tuple", i, got, v)
								}
							}
							cells := 3
							if vector {
								cells++
							}
							if !bytes.Equal(args[8*cells:], bytes.Repeat([]byte{0x5a}, len(args)-8*cells)) || !bytes.Equal(results[8*len(want):], bytes.Repeat([]byte{0xa5}, len(results)-8*len(want))) || !bytes.Equal(dir, before) || !bytes.Equal(trap, make([]byte, len(trap))) {
								t.Fatal("module descriptor/transport sentinels")
							}
						}
						if n := testing.AllocsPerRun(10, func() {
							write(1)
							if err := e.CallPrepared(base+uintptr(c.functions[0].Entry), args, j.LinMemBase(), trap, results); err != nil {
								panic(err)
							}
						}); n != 0 {
							t.Fatal("closed warm call allocations", n)
						}
						// A missing descriptor directory must trap at the actual ref.func before
						// entering an internal function or writing the result destination.
						write(1)
						j.SetFuncRefDesc(0)
						err = e.CallPrepared(base+uintptr(c.functions[0].Entry), args, j.LinMemBase(), trap, results)
						var tr *coreruntime.TrapError
						pc := uint32(bytes.Index(m.Code[0].BodyBytes, []byte{0xd2, 1})) + m.Code[0].LocalDeclBytes
						if !errors.As(err, &tr) || tr.Code != coreruntime.TrapCode(productionTrapCode(trapIndirectBounds)) || binary.LittleEndian.Uint64(trap[16:]) != uint64(pc)<<32|1 || !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, len(results))) {
							t.Fatal("closed descriptor trap/source/prewrite", err, pc, trap[16:24])
						}
						j.SetFuncRefDesc(thunkPointerRV64(dir))
						write(1)
						if err := e.CallPrepared(base+uintptr(c.functions[0].Entry), args, j.LinMemBase(), trap, results); err != nil || binary.LittleEndian.Uint64(args) != uint64(thunkPointerRV64(dir[2*coreruntime.FuncRefDescBytes:])) || !bytes.Equal(dir, before) {
							t.Fatal("closed descriptor trap reuse", err)
						}
					})
				}
			}
		}
	}
}
