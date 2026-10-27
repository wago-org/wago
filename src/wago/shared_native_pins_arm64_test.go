//go:build (linux || darwin) && arm64 && !tinygo

package wago

import (
	"bytes"
	"testing"
)

func TestSharedSingleNativePinsAndGoFallback(t *testing.T) {
	images := map[string][]byte{}
	for _, mode := range []string{"0", "1"} {
		t.Setenv("WAGO_ARM64_SHARED_NATIVE_PINS", mode)
		cfg := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit)
		c, err := cfg.Compile(fixedSharedHostLoopModule(false))
		if err != nil {
			t.Fatal(err)
		}
		var image bytes.Buffer
		if _, err = c.WriteCodeTo(&image); err != nil {
			t.Fatal(err)
		}
		images[mode] = append([]byte(nil), image.Bytes()...)
		for index, code := range []*Compiled{c} {
			for _, kind := range []string{"go", "leaf", "pure-helper"} {
				native := kind == "leaf"
				calls := 0
				var callback any = nativeLeafIncrement
				if kind == "pure-helper" {
					callback = sharedNativePinsPureHelper
				}
				if kind == "go" {
					callback = func(v int32) int32 { calls++; return v + 1 }
				}
				in, err := Instantiate(code, InstantiateOptions{Imports: testImports("env.step", callback)})
				if err != nil {
					t.Fatal(err)
				}
				if actual := in.executionFlags.Load()&executionFlagNativeScalarLeaf != 0; actual != (native && index == 0 && !codeProfileEnabled) {
					t.Fatalf("native=%t artifact=%d admitted=%t", native, index, actual)
				}
				wantCalls := 0
				for _, n := range []uint32{0, 1, 17, 129, 65536} {
					count := uint64(n)
					if count == 0 {
						count = 1
					}
					want := uint64(uint32(count * (count + 1) / 2))
					got, err := in.Invoke("run", uint64(n))
					if err != nil || len(got) != 1 || got[0] != want {
						t.Fatalf("mode=%s native=%t artifact=%d n=%d got=%v,%v want=%d", mode, native, index, n, got, err, want)
					}
					wantCalls += int(count)
					if kind == "go" && calls != wantCalls {
						t.Fatalf("calls=%d want=%d", calls, wantCalls)
					}
				}
				in.Close()
			}
			if code != c {
				code.Close()
			}
		}
		c.Close()
	}
	if !codeProfileEnabled && bytes.Equal(images["0"], images["1"]) {
		t.Fatal("native pin selector did not change shared loop")
	}
}

//go:norace
//go:noinline
func sharedNativePinsHelper(v int32) int32 { return v + 1 }

//go:norace
//go:noinline
func sharedNativePinsPureHelper(v int32) int32 { return sharedNativePinsHelper(v) }
