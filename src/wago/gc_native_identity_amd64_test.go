//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import "testing"

func gcIdentityRefTestModule() []byte {
	global := []byte{0x6d, 1, 0x41, 7, 0xfb, 0, 0, 0x0b}
	body := []byte{
		1, 2, 0x7f, // i32 counter and sum
		0x02, 0x40, 0x03, 0x40,
		0x20, 1, 0x20, 0, 0x4f, 0x0d, 1,
		0x20, 2, 0x23, 0, 0xfb, 20, 0, 0x6a, 0x21, 2,
		0x20, 1, 0x41, 1, 0x6a, 0x21, 1, 0x0c, 0,
		0x0b, 0x0b, 0x20, 2, 0x0b,
	}
	return gcInstructionBenchmarkModule(body, global)
}

// Keep both identity operations correct across collection and collector modes.
func TestGCNativeIdentityCastAndTest(t *testing.T) {
	requireCompleteCore3Backend(t)
	for _, tc := range []struct {
		name string
		data []byte
	}{{"cast", gcRefCastInstructionBenchmarkModule()}, {"test", gcIdentityRefTestModule()}} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), tc.data)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			for _, cfg := range []GCConfig{{VerifyAfterCollect: true}, {Profile: GCProfileTiny, TinyHeapBytes: 256, TinyBlockBytes: 16, VerifyAfterCollect: true}} {
				in, err := Instantiate(c, InstantiateOptions{GC: cfg})
				if err != nil {
					t.Fatal(err)
				}
				func() {
					defer in.Close()
					for i := 0; i < 3; i++ {
						got, err := in.Invoke("run", 257)
						if err != nil || len(got) != 1 || got[0] != 257 {
							t.Fatalf("identity %s = %v, %v; want [257]", tc.name, got, err)
						}
						if err := in.CollectGC(); err != nil {
							t.Fatal(err)
						}
					}
				}()
			}
		})
	}
}

func BenchmarkGCRefTestIdentityInstruction(b *testing.B) {
	requireCompleteCore3Backend(b)
	benchmarkGCInstructionLoop(b, gcIdentityRefTestModule(), 1)
}
