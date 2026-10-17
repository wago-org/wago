//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func gcDeadGroupModule(n, fields int, kind string) []byte {
	fieldBytes := []byte{0x5f}
	fieldBytes = append(fieldBytes, wasmtest.ULEB(uint32(fields))...)
	widths := []byte{0x7f, 0x7e, 0x7d, 0x7c}
	for i := 0; i < fields; i++ {
		fieldBytes = append(fieldBytes, widths[i%4], 1)
	}
	leaf := []byte{0}
	if kind == "local" {
		leaf = []byte{1, 1, 0x7f}
	}
	if kind == "prefix" {
		leaf = append(leaf, 0x01)
	}
	for i := 0; i < n; i++ {
		idx := byte(0)
		if kind == "mixed" {
			idx = byte(i % 2)
		}
		leaf = append(leaf, 0xfb, 1, idx, 0x1a)
		if i == 0 && kind == "gap" {
			leaf = append(leaf, 0x01)
		}
	}
	leaf = append(leaf, 0x0b)
	// Keep a live numeric-field object in the caller across every group call.
	run := []byte{1, 1, 0x63, 0}
	for i := 0; i < fields; i++ {
		switch widths[i%4] {
		case 0x7f:
			run = append(run, 0x41, 0)
			if i == 0 {
				run = run[:len(run)-1]
				run = append(run, 0xc9, 0x00)
			}
		case 0x7e:
			run = append(run, 0x42, 0)
		case 0x7d:
			run = append(run, 0x43, 0, 0, 0, 0)
		case 0x7c:
			run = append(run, 0x44, 0, 0, 0, 0, 0, 0, 0, 0)
		}
	}
	run = append(run, 0xfb, 0, 0, 0x21, 1, 0x03, 0x40, 0x10, 0, 0x20, 0, 0x41, 1, 0x6b, 0x22, 0, 0x0d, 0, 0x0b, 0x20, 1, 0xfb, 2, 0, 0, 0x0b)
	types := [][]byte{fieldBytes}
	functionBase := byte(1)
	if kind == "mixed" {
		types = append(types, []byte{0x5f, 4, 0x7f, 1, 0x7e, 1, 0x7d, 1, 0x7c, 1})
		functionBase = 2
	}
	types = append(types, wasmtest.FuncType(nil, nil), wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))
	if kind == "only" {
		return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(3, wasmtest.Vec([]byte{functionBase})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("leaf", 0, 0))), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(leaf))), leaf...))))
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec([]byte{functionBase}, []byte{functionBase + 1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("leaf", 0, 0), wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(leaf))), leaf...), append(wasmtest.ULEB(uint32(len(run))), run...))),
	)
}

func TestGCDeadDefaultGroup(t *testing.T) {
	for _, n := range []int{1, 2, 3, 4, 5} {
		for _, fields := range []int{1, 4, 16, 17} {
			t.Run(fmt.Sprintf("n%d/fields%d", n, fields), func(t *testing.T) {
				c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcDeadGroupModule(n, fields, "plain"))
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				c = publicArtifactRoundTrip(t, c)
				defer c.Close()
				for _, profile := range []GCProfile{GCProfileThroughput, GCProfileTiny} {
					for _, stress := range []bool{false, true} {
						in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{Profile: profile, CollectEveryAlloc: stress, StressNurseryBytes: 256, VerifyAfterCollect: true}})
						if err != nil {
							t.Fatal(err)
						}
						for range 3 {
							before := in.gc.Stats()
							got, err := in.Invoke("run", 64)
							if err != nil || len(got) != 1 || got[0] != 73 {
								t.Fatalf("profile%d stress%v: result%v err%v", profile, stress, got, err)
							}
							if charges := in.gc.Stats().Allocations - before.Allocations; charges != uint64(64*n+1) {
								t.Fatalf("charges%d want%d", charges, 64*n+1)
							}
							if profile == GCProfileThroughput && !stress && in.gc.Stats().YoungBytesCopied <= before.YoungBytesCopied {
								t.Fatal("caller root fixture did not copy a young object")
							}
							if err := in.CollectGC(); err != nil {
								t.Fatal(err)
							}
						}
						in.Close()
					}
				}
			})
		}
	}
}

func BenchmarkGCDeadDefaultGroup(b *testing.B) {
	for _, n := range []int{2, 3, 4} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcDeadGroupModule(n, 4, "plain"))
			if err != nil {
				b.Fatal(err)
			}
			defer c.Close()
			in, err := Instantiate(c, InstantiateOptions{})
			if err != nil {
				b.Fatal(err)
			}
			defer in.Close()
			if got, err := in.Invoke("run", 256); err != nil || len(got) != 1 || got[0] != 73 {
				b.Fatalf("preflight%v,%v", got, err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			b.ReportMetric(float64(len(c.code)), "code-B")
			for i := 0; i < b.N; i++ {
				if _, err := in.Invoke("run", 256); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestGCDeadDefaultGroupExhaustion(t *testing.T) {
	for _, n := range []int{3, 4, 5} {
		c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcDeadGroupModule(n, 1, "plain"))
		if err != nil {
			t.Fatal(err)
		}
		in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{Profile: GCProfileThroughput, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096, DisableCollection: true}})
		if err != nil {
			t.Fatal(err)
		}
		for range 2 {
			before := in.gc.Stats().Allocations
			if _, err := in.Invoke("run", 129); err == nil {
				t.Fatal("expected bounded heap exhaustion within one invocation")
			}
			if got := in.gc.Stats().Allocations - before; got != 128 {
				t.Fatalf("n%d: successful allocations%d want128", n, got)
			}
		}
		in.Close()
		c.Close()
	}
}

func BenchmarkGCDeadDefaultGroupCompile(b *testing.B) {
	for _, n := range []int{1, 4, 16, 256} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			data := gcDeadGroupModule(n, 4, "plain")
			cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				c, err := Compile(cfg, data)
				if err != nil {
					b.Fatal(err)
				}
				b.ReportMetric(float64(len(c.code)), "code-B")
				c.Close()
			}
		})
	}
}

func TestGCDeadDefaultGroupMixed(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcDeadGroupModule(4, 1, "mixed"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, profile := range []GCProfile{GCProfileThroughput, GCProfileTiny} {
		for _, stress := range []bool{false, true} {
			in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{Profile: profile, CollectEveryAlloc: stress, StressNurseryBytes: 256, VerifyAfterCollect: true}})
			if err != nil {
				t.Fatal(err)
			}
			before := in.gc.Stats().Allocations
			got, err := in.Invoke("run", 64)
			if err != nil || len(got) != 1 || got[0] != 73 {
				t.Fatalf("mixed%v,%v", got, err)
			}
			if count := in.gc.Stats().Allocations - before; count != 257 {
				t.Fatalf("mixed charges%d want257", count)
			}
			in.Close()
		}
	}
}

func TestGCDeadDefaultGroupStandalone(t *testing.T) {
	for _, fields := range []int{1, 4, 16} {
		c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcDeadGroupModule(4, fields, "only"))
		if err != nil {
			t.Fatal(err)
		}
		if c.nativeGCABIRequirement() < 8 {
			t.Fatal("helper40 requires artifact-visible GC ABI8 or later")
		}
		for _, profile := range []GCProfile{GCProfileThroughput, GCProfileTiny} {
			in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{Profile: profile, VerifyAfterCollect: true, StressNurseryBytes: 256}})
			if err != nil {
				t.Fatal(err)
			}
			for range 32 {
				before := in.gc.Stats().Allocations
				if _, err := in.Invoke("leaf"); err != nil {
					t.Fatalf("fields%d profile%d:%v", fields, profile, err)
				}
				if count := in.gc.Stats().Allocations - before; count != 4 {
					t.Fatalf("standalone charges%d", count)
				}
			}
			in.Close()
		}
		c.Close()
	}
}
