//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func gcPureIfModule(reads, regions int, kind string) []byte {
	body := []byte{2, 1, 0x63, 0, 1, 0x7f, 0x23, 0, 0x21, 2, 0x03, 0x40}
	for site := 0; site < regions; site++ {
		if kind == "call" {
			body = append(body, 0x41, 7, 0xfb, 0, 0, 0x21, 2)
		}
		body = append(body, 0x20, 2, 0xfb, 2, 0, 0, 0x1a, 0x20, 1, 0x04, 0x40)
		switch kind {
		case "assign":
			body = append(body, 0x23, 1, 0x21, 2)
		case "call":
			body = append(body, 0x10, 0)
		case "nested":
			body = append(body, 0x02, 0x40, 0x41, 3, 0x1a, 0x0b)
		case "branch":
			body = append(body, 0x0c, 0)
		case "long":
			for i := 0; i < 40; i++ {
				body = append(body, 0x41, 1, 0x1a)
			}
		default:
			body = append(body, 0x20, 0, 0x41, 3, 0x73, 0x1a)
		}
		if kind != "noelse" {
			body = append(body, 0x05, 0x20, 1, 0x41, 5, 0x6a, 0x1a)
		}
		body = append(body, 0x0b)
		for i := 0; i < reads; i++ {
			body = append(body, 0x20, 3, 0x20, 2, 0xfb, 2, 0, 0, 0x6a, 0x21, 3)
		}
	}
	body = append(body, 0x20, 0, 0x41, 1, 0x6b, 0x22, 0, 0x0d, 0, 0x0b, 0x20, 3, 0x0b)
	init := wasmtest.Code([]byte{0x41, 7, 0xfb, 0, 0, 0x24, 0, 0x41, 11, 0xfb, 0, 0, 0x24, 1, 0x0b})
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 1}, wasmtest.FuncType(nil, nil), wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b}, []byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("init", 0, 0), wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(init, append(wasmtest.ULEB(uint32(len(body))), body...))))
}

func TestGCPureIf(t *testing.T) {
	for _, kind := range []string{"pure", "noelse", "assign", "call", "nested", "branch", "long"} {
		for _, regions := range []int{1, 4, 20} {
			t.Run(fmt.Sprintf("%s/%d", kind, regions), func(t *testing.T) {
				c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcPureIfModule(4, regions, kind))
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()

				iterations := 100
				if kind == "call" {
					// Every allocation triggers heap verification. Keep this case small
					// because promoted objects accumulate across the loop.
					iterations = 4
				}
				for _, profile := range []GCProfile{GCProfileThroughput, GCProfileTiny} {
					cfg := GCConfig{Profile: profile, NurseryBytes: 4096, VerifyAfterCollect: true}
					if kind == "call" {
						cfg.CollectEveryAlloc = true
						cfg.StressNurseryBytes = 64
					}
					in, err := Instantiate(c, InstantiateOptions{GC: cfg})
					if err != nil {
						t.Fatal(err)
					}
					for _, cond := range []uint64{0, 1, 0, 1} {
						if _, err = in.Invoke("init"); err != nil {
							t.Fatal(err)
						}
						before := in.gc.Stats()
						got, err := in.Invoke("run", uint64(iterations), cond)
						want := uint64(iterations * 4 * regions * 7)
						if kind == "assign" && cond != 0 {
							want = uint64(iterations * 4 * regions * 11)
						}
						if err != nil || len(got) != 1 || got[0] != want {
							t.Fatalf("profile%d cond%d: %v,%v want%d", profile, cond, got, err, want)
						}
						if kind == "call" && cond != 0 && profile == GCProfileThroughput {
							after := in.gc.Stats()
							if after.MinorCollections <= before.MinorCollections || after.YoungBytesCopied <= before.YoungBytesCopied {
								t.Fatal("call fixture did not move live objects")
							}
						}
						if err = in.CollectGC(); err != nil {
							t.Fatal(err)
						}
					}
					in.Close()
				}
			})
		}
	}
}

func BenchmarkGCPureIf(b *testing.B) {
	for _, reads := range []int{1, 4, 16} {
		for _, cond := range []uint64{0, 1} {
			b.Run(fmt.Sprintf("%d/%d", reads, cond), func(b *testing.B) {
				c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcPureIfModule(reads, 1, "pure"))
				if err != nil {
					b.Fatal(err)
				}
				defer c.Close()
				in, err := Instantiate(c, InstantiateOptions{})
				if err != nil {
					b.Fatal(err)
				}
				defer in.Close()
				if _, err = in.Invoke("init"); err != nil {
					b.Fatal(err)
				}
				if got, err := in.Invoke("run", 1024, cond); err != nil || len(got) != 1 || got[0] != uint64(1024*reads*7) {
					b.Fatalf("preflight: %v,%v", got, err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				b.ReportMetric(float64(len(c.code)), "code-B")
				for i := 0; i < b.N; i++ {
					got, err := in.Invoke("run", 1024, cond)
					if err != nil || len(got) != 1 || got[0] != uint64(1024*reads*7) {
						b.Fatalf("%v,%v", got, err)
					}
				}
			})
		}
	}
}

func BenchmarkGCPureIfCompile(b *testing.B) {
	for _, regions := range []int{1, 4, 16, 256} {
		b.Run(fmt.Sprint(regions), func(b *testing.B) {
			data := gcPureIfModule(4, regions, "pure")
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
