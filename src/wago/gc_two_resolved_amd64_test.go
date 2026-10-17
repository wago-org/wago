//go:build linux && amd64 && !tinygo

package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func gcRetainedResolverSource(objects, read int, kind string) int {
	threshold, reverse := 0, false
	switch kind {
	case "phase":
		threshold = 8
	case "phase1":
		threshold = 1
	case "phase2":
		threshold = 2
	case "reverse1":
		threshold, reverse = 1, true
	case "reverse8":
		threshold, reverse = 8, true
	default:
		return read % objects
	}
	source := 0
	if read >= threshold {
		source = 1
	}
	if reverse {
		source = 1 - source
	}
	return source
}

func gcRetainedResolverModule(objects, reads int, kind string) []byte {
	init := []byte{0}
	for i := 0; i < 3; i++ {
		init = append(init, 0x41, byte(7+4*i), 0xfb, 0, 0, 0x24, byte(i))
	}
	init = append(init, 0x0b)
	sum, iter := byte(objects+1), byte(objects+2)
	body := []byte{2, byte(objects), 0x63, 0, 2, 0x7f}
	for i := 0; i < objects; i++ {
		body = append(body, 0x23, byte(3-objects+i), 0x21, byte(i+1))
	}
	if kind == "call" {
		body = append(body, 0x41, 15, 0xfb, 0, 0, 0x22, byte(objects), 0x24, 2)
	}
	body = append(body, 0x02, 0x40, 0x03, 0x40, 0x20, iter, 0x20, 0, 0x4f, 0x0d, 1)
	for j := 0; j < reads; j++ {
		if j == 4 {
			switch kind {
			case "assign":
				body = append(body, 0x23, 0, 0x21, 1)
			case "store":
				body = append(body, 0x20, 1, 0x41)
				body = append(body, wasmtest.SLEB32(99)...)
				body = append(body, 0xfb, 5, 0, 0)
			case "call":
				body = append(body, 0x10, 0)
			case "if":
				body = append(body, 0x20, 0, 0x04, 0x40, 0x41, 1, 0x1a, 0x05, 0x41, 2, 0x1a, 0x0b)
			}
		}
		object := gcRetainedResolverSource(objects, j, kind)
		body = append(body, 0x20, sum, 0x20, byte(1+object), 0xfb, 2, 0, 0, 0x6a, 0x21, sum)
	}
	body = append(body, 0x20, iter, 0x41, 1, 0x6a, 0x21, iter, 0x0c, 0, 0x0b, 0x0b, 0x20, sum, 0x0b)
	globals := make([][]byte, 3)
	for i := range globals {
		globals[i] = []byte{0x63, 0, 1, 0xd0, 0, 0x0b}
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 1}, wasmtest.FuncType(nil, nil), wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2})),
		wasmtest.Section(6, wasmtest.Vec(globals...)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("init", 0, 0), wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(init))), init...), append(wasmtest.ULEB(uint32(len(body))), body...))))
}

func gcRetainedResolverWant(objects, reads int, kind string) uint64 {
	var sum uint64
	for j := 0; j < reads; j++ {
		obj := gcRetainedResolverSource(objects, j, kind)
		v := uint64(7 + 4*(3-objects+obj))
		if j >= 4 && obj == 0 {
			if kind == "assign" {
				v = 7
			}
			if kind == "store" {
				v = 99
			}
		}
		sum += v
	}
	return sum
}

func TestGCRetainedResolver(t *testing.T) {
	for _, objects := range []int{1, 2, 3} {
		kinds := []string{"plain", "if", "assign", "store", "call"}
		if objects >= 2 {
			kinds = append(kinds, "phase", "phase1", "phase2", "reverse1", "reverse8")
		}
		for _, kind := range kinds {
			t.Run(fmt.Sprintf("objects%d/%s", objects, kind), func(t *testing.T) {
				c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcRetainedResolverModule(objects, 16, kind))
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				c = publicArtifactRoundTrip(t, c)
				defer c.Close()
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
					if _, err = in.Invoke("init"); err != nil {
						t.Fatal(err)
					}
					before := in.gc.Stats()
					got, err := in.Invoke("run", 1)
					want := gcRetainedResolverWant(objects, 16, kind)
					if err != nil || len(got) != 1 || got[0] != want {
						t.Fatalf("profile%d got%v err%v want%d", profile, got, err, want)
					}
					after := in.gc.Stats()
					if kind == "call" && profile == GCProfileThroughput && (after.MinorCollections <= before.MinorCollections || after.YoungBytesCopied <= before.YoungBytesCopied) {
						t.Fatalf("call did not move live nursery object before%+v after%+v", before, after)
					}
					in.Close()
				}
			})
		}
	}
}

func BenchmarkGCRetainedResolver(b *testing.B) {
	for _, objects := range []int{1, 2, 3} {
		b.Run(fmt.Sprint(objects), func(b *testing.B) {
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcRetainedResolverModule(objects, 16, "plain"))
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
			want := gcRetainedResolverWant(objects, 16, "plain") * 1024
			for i := 0; i < 3; i++ {
				got, err := in.Invoke("run", 1024)
				if err != nil || len(got) != 1 || got[0] != want {
					b.Fatalf("preflight%v %v", got, err)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				got, err := in.Invoke("run", 1024)
				if err != nil || len(got) != 1 || got[0] != want {
					b.Fatalf("got%v %v", got, err)
				}
			}
			b.ReportMetric(float64(c.CodeSize()), "native-B")
		})
	}
}

func BenchmarkGCRetainedResolverPhase(b *testing.B) {
	for _, kind := range []string{"phase", "phase1", "phase2", "reverse1", "reverse8"} {
		b.Run(kind, func(b *testing.B) {
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcRetainedResolverModule(2, 16, kind))
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
			want := gcRetainedResolverWant(2, 16, kind) * 1024
			for i := 0; i < 3; i++ {
				got, err := in.Invoke("run", 1024)
				if err != nil || len(got) != 1 || got[0] != want {
					b.Fatalf("preflight%v %v", got, err)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				got, err := in.Invoke("run", 1024)
				if err != nil || len(got) != 1 || got[0] != want {
					b.Fatalf("got%v %v", got, err)
				}
			}
			b.ReportMetric(float64(c.CodeSize()), "native-B")
		})
	}
}

func BenchmarkGCRetainedResolverCompile(b *testing.B) {
	for _, reads := range []int{16, 64, 256} {
		b.Run(fmt.Sprint(reads), func(b *testing.B) {
			data := gcRetainedResolverModule(2, reads, "plain")
			config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				c, err := Compile(config, data)
				if err != nil {
					b.Fatal(err)
				}
				b.ReportMetric(float64(c.CodeSize()), "native-B")
				c.Close()
			}
		})
	}
}
