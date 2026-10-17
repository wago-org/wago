//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestRV64PublicReferenceFunctionStateSourceArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, tails := range []bool{false, true} {
				for _, wideFirst := range []bool{false, true} {
					key := fmt.Sprintf("reference-state-v%t-t%t-w%t", vector, tails, wideFirst)
					t.Run(fmt.Sprintf("artifact%t-%s", artifact, key), func(t *testing.T) {
						c := rv64NativeTailCompile(t, key, wasmtest.ClosedReferenceState(vector, tails, wideFirst), artifact)
						if !c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || !c.requiredFeatures.IsEnabled(CoreFeatureMultiMemory|CoreFeatureMemory64) || len(c.ValueTypes) != 2 {
							t.Fatal("closed state metadata", c.requiredFeatures, len(c.ValueTypes))
						}
						rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
						defer rt.Close()
						in := rv64NativeTokenInstantiate(t, rt, c, NewImports())
						if len(c.Globals[1].InitExpr) == 0 || c.Globals[1].Bits != 0 || readGlobalObject(in.globalCells[1], ValI32) != 11 {
							t.Fatal("canonical global-dependent initializer")
						}
						for _, name := range []string{"memory", "arbitrary"} {
							if memory, err := in.ExportedMemory(name); memory != nil || err == nil {
								t.Fatal("unexported owned memory escaped", name, err)
							}
						}
						for index, memory := range in.memoryDir.memories {
							if got := memory.UnsafeBytes()[32:34]; !bytes.Equal(got, []byte{byte(0x21 + index), byte(0x41 + index)}) {
								t.Fatal("active data initialization", index, got)
							}
						}
						args := []uint64{0xdeadbeeffffffffd, 0x7ff0000000000001}
						payload := slices.Clone(args[1:])
						if vector {
							args = append(args, 0x80000000deadbeef)
							payload = append(payload, args[2])
						}
						want := append(slices.Clone(payload), 6, 0)
						for range 3 {
							if got, err := in.Invoke("run", args...); err != nil || !slices.Equal(got, want) {
								t.Fatal("stateful reference transport", got, err)
							}
						}
						fn, err := in.WasmFunc("run")
						if err != nil {
							t.Fatal(err)
						}
						session, err := fn.OpenSession()
						if err != nil {
							t.Fatal(err)
						}
						got, err := session.Invoke(args...)
						session.Close()
						if err != nil || !slices.Equal(got, want) {
							t.Fatal("prepared owned state", got, err)
						}
						if a := testing.AllocsPerRun(10, func() {
							got, err := in.Invoke("run", args...)
							if err != nil || !slices.Equal(got, want) {
								panic("warm owned reference state")
							}
						}); a != 0 {
							t.Fatal("warm allocations", a)
						}
						payloadBytes := make([]byte, 0, len(payload)*8)
						for _, bits := range payload {
							payloadBytes = binary.LittleEndian.AppendUint64(payloadBytes, bits)
						}
						for index, memory := range in.memoryDir.memories {
							data := memory.UnsafeBytes()
							if len(data) != 3*65536 || !bytes.Equal(data[48:52], []byte{0x12, 0x34, 0x56, 0x78}) || !bytes.Equal(data[64:64+len(payloadBytes)], payloadBytes) {
								t.Fatal("state after growth/bulk/global transport", index)
							}
							width := len(payloadBytes)
							before := bytes.Repeat([]byte{0x55}, width)
							copy(data[len(data)-width:], before)
							storeArgs := append([]uint64{uint64(len(data) - width + 1)}, payload...)
							_, err := in.Invoke(fmt.Sprintf("store%d", index), storeArgs...)
							var trap *coreruntime.TrapError
							if !errors.As(err, &trap) || trap.Code != coreruntime.TrapLinMemOutOfBounds || !bytes.Equal(data[len(data)-width:], before) {
								t.Fatal("partial terminal store", index, err)
							}
							wide := (index == 0) == wideFirst
							if wide {
								beforeStore := slices.Clone(data)
								for i := range payload {
									storeArgs[i+1] = ^payload[i]
								}
								for _, address := range []uint64{1<<32 | 64, 1 << 63, ^uint64(0)} {
									storeArgs[0] = address
									_, err := in.Invoke(fmt.Sprintf("store%d", index), storeArgs...)
									if !errors.As(err, &trap) || trap.Code != coreruntime.TrapLinMemOutOfBounds || !bytes.Equal(data, beforeStore) {
										t.Fatal("memory64 address truncation/carry", address, err)
									}
								}
							}
							copy(storeArgs[1:], payload)
							storeArgs[0] = uint64(len(data) - width)
							if !wide {
								storeArgs[0] |= 0xcafe000000000000
							}
							if _, err := in.Invoke(fmt.Sprintf("store%d", index), storeArgs...); err != nil {
								t.Fatal("terminal store reuse", err)
							}
							if got, err := in.Invoke(fmt.Sprintf("load%d", index), storeArgs[0]); err != nil || !slices.Equal(got, payload) {
								t.Fatal("terminal load reuse", got, err)
							}
							failure := uint64(0xffffffff)
							if wide {
								failure = ^uint64(0)
							}
							if got, err := in.Invoke(fmt.Sprintf("grow%d", index), failure); err != nil || !slices.Equal(got, []uint64{failure}) {
								t.Fatal("failed growth", got, err)
							}
							if got, err := in.Invoke(fmt.Sprintf("size%d", index)); err != nil || !slices.Equal(got, []uint64{3}) {
								t.Fatal("failed grow changed size", got, err)
							}
						}
						if got, err := in.Invoke("run", args...); err != nil || !slices.Equal(got, want) {
							t.Fatal("post-trap state reuse", got, err)
						}
					})
				}
			}
		}
	}
}

func rv64ReferenceStateConsumer(vector bool, route string) []byte {
	typ := wasm.F64
	if vector {
		typ = wasm.V128
	}
	signature := wasmtest.FuncType([]wasm.ValType{wasm.I32, typ}, []wasm.ValType{typ, wasm.I32, wasm.I32})
	imports := wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("target")...), 0, 0))
	body := []byte{0x20, 0, 0x20, 1}
	switch route {
	case "ordinary":
		body = append(body, 0x10, 0)
	case "static-tail":
		body = append(body, 0x12, 0)
	case "reference-call":
		body = append(body, 0xd2, 0, 0x14, 0)
	case "reference-tail":
		body = append(body, 0xd2, 0, 0x15, 0)
	}
	probe := []byte{}
	for i := 0; i < 2; i++ {
		read := append([]byte{0x41}, wasmtest.SLEB32(80)...)
		read = append(read, rv64M64Arg([]byte{0x2d}, uint32(i), 0)...)
		probe = append(probe, read...)
		if route == "ordinary" || route == "reference-call" {
			body = append(body, read...)
			body = append(body, 0x41)
			body = append(body, wasmtest.SLEB32(int32(0xa1+i))...)
			body = append(body, 0x47, 4, 0x40, 0, 0x0b)
		}
	}
	if route == "ordinary" || route == "reference-call" {
		body = append(body, 0x23, 0, 0x41)
		body = append(body, wasmtest.SLEB32(113)...)
		body = append(body, 0x47, 4, 0x40, 0, 0x0b)
	}
	probe = append(probe, 0x23, 0, 0x0b)
	body = append(body, 0x0b)
	offset := append([]byte{0x41}, wasmtest.SLEB32(80)...)
	offset = append(offset, 0x0b)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(signature, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}))), wasmtest.Section(2, imports), wasmtest.Section(3, []byte{2, 0, 1}), wasmtest.Section(5, []byte{2, 1, 1, 3, 1, 1, 3}), wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, true, append(append([]byte{0x41}, wasmtest.SLEB32(113)...), 0x0b)))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1), wasmtest.ExportEntry("probe", 0, 2))), wasmtest.Section(9, []byte{1, 3, 0, 1, 0}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body), wasmtest.Code(probe))), wasmtest.Section(11, wasmtest.Vec(rv64M64Data(0, offset, []byte{0xa1}), rv64M64Data(1, offset, []byte{0xa2}))))
}

func TestRV64PublicReferenceFunctionStateForeignOwner(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, route := range []string{"ordinary", "static-tail", "reference-call", "reference-tail"} {
				t.Run(fmt.Sprintf("artifact%t-vector%t-%s", artifact, vector, route), func(t *testing.T) {
					pc := rv64NativeTailCompile(t, fmt.Sprintf("reference-state-v%t-ttrue-wtrue", vector), wasmtest.ClosedReferenceState(vector, true, true), artifact)
					cc := rv64NativeTailCompile(t, fmt.Sprintf("reference-state-consumer-v%t-%s", vector, route), rv64ReferenceStateConsumer(vector, route), artifact)
					rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
					defer rt.Close()
					producer := rv64NativeTokenInstantiate(t, rt, pc, NewImports())
					target, err := producer.ExportedFunc("run")
					if err != nil {
						t.Fatal(err)
					}
					caller := rv64NativeTokenInstantiate(t, rt, cc, testImports("env.target", target))
					primary, secondary := producer.memoryDir.memories[0], producer.memoryDir.memories[1]
					if err := producer.Close(); err != nil {
						t.Fatal(err)
					}
					if err := pc.Close(); err != nil {
						t.Fatal(err)
					}
					if primary.jobMemory() == nil || secondary.jobMemory() == nil {
						t.Fatal("attached state retired too early")
					}
					args := []uint64{0xcafe000080000001, 0x7ff0000000000001}
					payload := slices.Clone(args[1:])
					if vector {
						args = append(args, 0x80000000deadbeef)
						payload = append(payload, args[2])
					}
					want := append(slices.Clone(payload), 0x8000000a, 0)
					fn, err := caller.WasmFunc("call")
					if err != nil {
						t.Fatal(err)
					}
					session, err := fn.OpenSession()
					if err != nil {
						t.Fatal(err)
					}
					for range 3 {
						got, err := session.Invoke(args...)
						if err != nil || !slices.Equal(got, want) {
							session.Close()
							t.Fatal("stateful foreign call", got, err)
						}
					}
					session.Close()
					if got, err := caller.Invoke("probe"); err != nil || !slices.Equal(got, []uint64{0xa1, 0xa2, 113}) {
						t.Fatal("foreign context restoration", got, err)
					}
					for _, memory := range []*Memory{primary, secondary} {
						data := memory.UnsafeBytes()
						if len(data) != 3*65536 || !bytes.Equal(data[48:52], []byte{0x12, 0x34, 0x56, 0x78}) {
							t.Fatal("closed producer state/growth")
						}
					}
					if route != "reference-tail" {
						if a := testing.AllocsPerRun(10, func() {
							got, err := caller.Invoke("call", args...)
							if err != nil || !slices.Equal(got, want) {
								panic("warm stateful foreign call")
							}
						}); a != 0 {
							t.Fatal("warm stateful foreign allocation", a)
						}
					}
					if err := caller.Close(); err != nil {
						t.Fatal(err)
					}
					if producer.jm != nil || primary.jobMemory() != nil || secondary.jobMemory() != nil {
						t.Fatal("last attachment retained owned state")
					}
				})
			}
		}
	}
}
