//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"

	"testing"
)

func rv64CompactExternAggregateSource(vector, array bool) []byte {
	container := []byte{0x5f, 1, 0x6f, 1}
	create, get := []byte{0xfb, 0, 1}, []byte{0xfb, 2, 1, 0}
	if array {
		container = []byte{0x5e, 0x6f, 1}
		create = []byte{0xfb, 8, 1, 1}
		get = []byte{0x41, 0, 0xfb, 11, 1}
	}
	body := []byte{0x20, 0, 0xfb, 0, 0, 0xfb, 27}
	body = append(body, create...)
	body = append(body, get...)
	body = append(body, 0xfb, 1, 0, 0x1a, 0xfb, 26, 0xfb, 22, 0, 0xfb, 2, 0, 0, 0x0b)
	if vector {
		prefix := append([]byte{0xfd, 12}, make([]byte, 16)...)
		body = append(append(prefix, 0x1a), body...)
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 0}, container, wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, []byte{1, 2}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}
func TestRV64CompactExternAggregateSourcePrivate(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, array := range []bool{false, true} {
			c, err := NewRuntimeConfig().Compile(rv64CompactExternAggregateSource(vector, array))
			if err != nil {
				t.Fatal(vector, array, err)
			}
			defer c.Close()

			for artifact, candidate := range []*Compiled{c} {
				for _, profile := range []gc.Profile{gc.ProfileThroughput, gc.ProfileTiny} {
					t.Run(fmt.Sprintf("vector%t/array%t/artifact%t/profile%d", vector, array, artifact != 0, profile), func(t *testing.T) {
						in, err := Instantiate(candidate, InstantiateOptions{GC: GCConfig{Profile: profile, CollectEveryAlloc: true, StressNurseryBytes: 128, VerifyAfterCollect: true}})
						if err != nil {
							t.Fatal(err)
						}
						defer in.Close()
						for i := uint64(0); i < 32; i++ {
							got, err := in.Invoke("run", i+42)
							if err != nil || len(got) != 1 || got[0] != i+42 {
								t.Fatal("extracted extern after collection", got, err)
							}
						}
						if err := in.CollectGC(); err != nil {
							t.Fatal(err)
						}
						state, _ := compactExternCollectors.Load(in.gc)
						if len(state.shells) != 0 || len(state.foreign) != 0 {
							t.Fatal("returned numeric values retained extern metadata")
						}
					})
				}
			}
		}
	}
}

func rv64CompactExternExceptionSource(vector bool) []byte {
	body := []byte{
		0x02, 0x69, 0x1f, 0x40, 1, byte(wasm.CatchAllRef), 0,
		0x20, 0, 0xfb, 0, 0, 0xfb, 27, 0x08, 0, 0x0b, 0, 0x0b, 0x21, 1,
		0x02, 0x6f, 0x1f, 0x40, 1, byte(wasm.CatchTag), 0, 0, 0x20, 1, 0x0a, 0x0b, 0, 0x0b,
		0xd0, 0x69, 0x21, 1, 0xfb, 1, 0, 0x1a, 0xfb, 26, 0xfb, 22, 0, 0xfb, 2, 0, 0, 0x0b,
	}
	if vector {
		prefix := append([]byte{0xfd, 12}, make([]byte, 16)...)
		body = append(append(prefix, 0x1a), body...)
	}
	raw := append([]byte{1, 1, 0x69}, body...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 0}, wasmtest.FuncType([]wasm.ValType{wasm.ExternRef}, nil), wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, []byte{1, 2}), wasmtest.Section(13, []byte{1, 0, 1}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(raw))), raw...))),
	)
}
func TestRV64CompactExternExceptionSourcePrivate(t *testing.T) {
	for _, vector := range []bool{false, true} {
		c, err := NewRuntimeConfig().Compile(rv64CompactExternExceptionSource(vector))
		if err != nil {
			t.Fatal(vector, err)
		}
		defer c.Close()

		for artifact, candidate := range []*Compiled{c} {
			for _, profile := range []gc.Profile{gc.ProfileThroughput, gc.ProfileTiny} {
				t.Run(fmt.Sprintf("vector%t/artifact%t/profile%d", vector, artifact != 0, profile), func(t *testing.T) {
					in, err := Instantiate(candidate, InstantiateOptions{GC: GCConfig{Profile: profile, CollectEveryAlloc: true, StressNurseryBytes: 128, VerifyAfterCollect: true}})
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					for i := uint64(0); i < 16; i++ {
						got, err := in.Invoke("run", i+42)
						if err != nil || len(got) != 1 || got[0] != i+42 {
							t.Fatal(got, err)
						}
					}
					if err := in.CollectGC(); err != nil {
						t.Fatal(err)
					}
					state, ok := compactExternCollectors.Load(in.gc)
					if !ok || len(state.shells) != 0 || len(state.foreign) != 0 {
						t.Fatal("captured extern metadata was not reclaimed")
					}
				})
			}
		}
	}
}

func rv64CompactExternLoosePersistentSource(vector bool, mode string) []byte {
	save := []byte{0x20, 0, 0x24, 0, 0x0b}
	run := []byte{0x23, 0, 0xd0, 0x6f, 0x24, 0}
	if mode != "global" {
		save = []byte{0x41, 0, 0x20, 0, 0x26, 0, 0x0b}
		run = []byte{0x41, 0, 0x25, 0}
		if mode == "fill" {
			run = append(run, 0x41, 0, 0xd0, 0x6f, 0x41, 1, 0xfc, 17, 0)
		} else {
			run = append(run, 0x41, 0, 0xd0, 0x6f, 0x26, 0)
		}
	}
	run = append(run, 0x10, 0, 0xfb, 1, 0, 0x1a, 0xfb, 26, 0xfb, 27, 0x0b)
	if vector {
		prefix := append([]byte{0xfd, 12}, make([]byte, 16)...)
		run = append(append(prefix, 0x1a), run...)
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 0}, wasmtest.FuncType(nil, nil), wasmtest.FuncType([]wasm.ValType{wasm.ExternRef}, nil), wasmtest.FuncType(nil, []wasm.ValType{wasm.ExternRef}))),
		wasmtest.Section(2, wasmtest.Vec(append(append(append(wasmtest.Name("env"), wasmtest.Name("release")...), 0), 1))),
		wasmtest.Section(3, []byte{2, 2, 3}), wasmtest.Section(4, []byte{1, 0x6f, 1, 1, 1}),
		wasmtest.Section(6, []byte{1, 0x6f, 1, 0xd0, 0x6f, 0x0b}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("save", 0, 1), wasmtest.ExportEntry("run", 0, 2))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(save), wasmtest.Code(run))),
	)
}
func TestRV64CompactExternLoosePersistentPrivate(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, mode := range []string{"global", "set", "fill"} {
			c, err := NewRuntimeConfig().Compile(rv64CompactExternLoosePersistentSource(vector, mode))
			if err != nil {
				t.Fatal(vector, mode, err)
			}
			defer c.Close()

			for artifact, candidate := range []*Compiled{c} {
				for _, profile := range []gc.Profile{gc.ProfileThroughput, gc.ProfileTiny} {
					t.Run(fmt.Sprintf("vector%t/%s/artifact%t/profile%d", vector, mode, artifact != 0, profile), func(t *testing.T) {
						rt := NewRuntime()
						defer rt.Close()
						mod, err := rt.Module(candidate)
						if err != nil {
							t.Fatal(err)
						}
						defer mod.Close()
						var token ExternRef
						released := false
						calls := 0
						imports := NewImports()
						imports.Function("env", "release", slotHostFunc(func(HostModule, []uint64, []uint64) { calls++; released = rt.ReleaseExternRef(token) }))
						in, err := rt.Instantiate(context.Background(), mod, WithImports(imports), WithGC(GCConfig{Profile: profile, CollectEveryAlloc: true, StressNurseryBytes: 128, VerifyAfterCollect: true}))
						if err != nil {
							t.Fatal(err)
						}
						defer in.Close()
						for i := 0; i < 8; i++ {
							token, err = rt.NewExternRef(i)
							if err != nil {
								t.Fatal(err)
							}
							if _, err = in.Invoke("save", token.token); err != nil {
								t.Fatal(err)
							}
							got, err := in.Invoke("run")
							if err != nil || len(got) != 1 || got[0] != token.token || released {
								t.Fatal("loose external lifetime", got, err, released)
							}
							if err := in.CollectGC(); err != nil {
								t.Fatal(err)
							}
							if !rt.ReleaseExternRef(token) {
								t.Fatal("dead native local retained host token")
							}
						}
						if calls != 8 {
							t.Fatal("release callbacks", calls)
						}
					})
				}
			}
		}
	}
}

func rv64CompactExternAggregateConstantSource(vector, array bool) []byte {
	container := []byte{0x5f, 1, 0x6f, 0}
	create, get := []byte{0xfb, 0, 1}, []byte{0xfb, 2, 1, 0}
	if array {
		container = []byte{0x5e, 0x6f, 0}
		create = []byte{0xfb, 8, 1, 1}
		get = []byte{0x41, 0, 0xfb, 11, 1}
	}
	init := []byte{0x63, 1, 0, 0x41, 42, 0xfb, 0, 0, 0xfb, 27}
	init = append(init, create...)
	init = append(init, 0x0b)
	run := []byte{0x23, 0}
	run = append(run, get...)
	run = append(run, 0xfb, 1, 0, 0x1a, 0xfb, 26, 0xfb, 22, 0, 0xfb, 2, 0, 0, 0x0b)
	if vector {
		prefix := append([]byte{0xfd, 12}, make([]byte, 16)...)
		run = append(append(prefix, 0x1a), run...)
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 0}, container, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, []byte{1, 2}), wasmtest.Section(6, wasmtest.Vec(init, []byte{0x63, 0, 0, 0xfb, 1, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(run))),
	)
}
func TestRV64CompactExternAggregateConstantPrivate(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, array := range []bool{false, true} {
			c, err := NewRuntimeConfig().Compile(rv64CompactExternAggregateConstantSource(vector, array))
			if err != nil {
				t.Fatal(vector, array, err)
			}
			defer c.Close()

			for artifact, candidate := range []*Compiled{c} {
				for _, profile := range []gc.Profile{gc.ProfileThroughput, gc.ProfileTiny} {
					t.Run(fmt.Sprintf("vector%t/array%t/artifact%t/profile%d", vector, array, artifact != 0, profile), func(t *testing.T) {
						in, err := Instantiate(candidate, InstantiateOptions{GC: GCConfig{Profile: profile, CollectEveryAlloc: true, StressNurseryBytes: 128, VerifyAfterCollect: true}})
						if err != nil {
							t.Fatal(err)
						}
						defer in.Close()
						if err := in.CollectGC(); err != nil {
							t.Fatal(err)
						}
						for i := 0; i < 8; i++ {
							got, err := in.Invoke("run")
							if err != nil || len(got) != 1 || got[0] != 42 {
								t.Fatal(got, err)
							}
						}
					})
				}
			}
		}
	}
}
