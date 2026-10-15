//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import "testing"

func gcConstructorForwardBytes() []byte {
	types := [][]byte{
		{0x50, 0x00, 0x5f, 0x01, 0x7f, 0x00},                   // open base: (struct (field i32))
		{0x50, 0x01, 0x00, 0x5f, 0x02, 0x7f, 0x00, 0x7f, 0x00}, // open child <: base
	}
	body := []byte{
		0x00,       // no locals
		0x20, 0x00, // local.get 0
		0x41, 0x07, // i32.const 7
		0xfb, 0x00, 0x01, // struct.new child
		0xfb, 0x16, 0x00, // ref.cast base
		0xfb, 0x02, 0x00, 0x00, // struct.get base field 0
		0x0b,
	}
	return gcInstructionBenchmarkModuleWithTypes(types, body)
}

func TestGCConstructorGetForwardExecution(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcConstructorForwardBytes())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()

	profiles := []struct {
		name string
		gc   GCConfig
	}{
		{name: "throughput", gc: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}},
		{name: "tiny", gc: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 64, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, VerifyAfterCollect: true}},
	}
	for _, tc := range profiles {
		t.Run(tc.name, func(t *testing.T) {
			instance, err := Instantiate(compiled, InstantiateOptions{GC: tc.gc})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			for i := 0; i < 1000; i++ {
				want := uint64(uint32(i*17 - 33))
				got, err := instance.Invoke("run", want)
				if err != nil || len(got) != 1 || got[0] != want {
					t.Fatalf("run iteration %d = %v, %v; want [%d]", i, got, err, want)
				}
			}
		})
	}
}
