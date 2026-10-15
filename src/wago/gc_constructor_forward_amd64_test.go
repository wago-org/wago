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

func gcFreshStructLocalForwardBytes(overwriteSource, mutate bool, rootLength byte) []byte {
	fieldMutability := byte(0x00)
	if mutate {
		fieldMutability = 0x01
	}
	types := [][]byte{
		{0x5f, 0x01, 0x7f, fieldMutability}, // (struct (field i32))
		{0x5e, 0x63, 0x00, 0x01},            // (array (mut (ref null 0)))
	}
	body := []byte{
		0x02, 0x01, 0x63, 0x01, 0x01, 0x63, 0x00, // locals: roots, item
		0x41, rootLength, 0xfb, 0x07, 0x01, 0x21, 0x01, // roots = array.new_default 1 (rootLength)
		0x20, 0x00, 0xfb, 0x00, 0x00, 0x21, 0x02, // item = struct.new 0 (param)
	}
	if overwriteSource {
		body = append(body, 0x20, 0x00, 0x41, 0x01, 0x6a, 0x21, 0x00)
	}
	body = append(body,
		0x20, 0x01, 0x41, 0x00, 0x20, 0x02, 0xfb, 0x0e, 0x01, // roots[0] = item
	)
	if mutate {
		body = append(body, 0x20, 0x02, 0x41, 0x07, 0xfb, 0x05, 0x00, 0x00) // item.field 0 = 7
	}
	body = append(body,
		0x20, 0x02, 0xfb, 0x02, 0x00, 0x00, // item.field 0
		0x0b,
	)
	return gcInstructionBenchmarkModuleWithTypes(types, body)
}

func TestGCFreshStructLocalGetForwardExecution(t *testing.T) {
	for _, tc := range []struct {
		name            string
		overwriteSource bool
		mutate          bool
		want            func(int) uint64
	}{
		{name: "forward", want: func(i int) uint64 { return uint64(uint32(i*17 - 33)) }},
		{name: "source overwritten", overwriteSource: true, want: func(i int) uint64 { return uint64(uint32(i*17 - 33)) }},
		{name: "field mutated", mutate: true, want: func(int) uint64 { return 7 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcFreshStructLocalForwardBytes(tc.overwriteSource, tc.mutate, 1))
			if err != nil {
				t.Fatal(err)
			}
			for _, gc := range []GCConfig{
				{CollectEveryAlloc: true, StressNurseryBytes: 128, VerifyAfterCollect: true},
				{Profile: GCProfileTiny, TinyHeapBytes: 128, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, VerifyAfterCollect: true},
			} {
				instance, err := Instantiate(compiled, InstantiateOptions{GC: gc})
				if err != nil {
					t.Fatal(err)
				}
				for i := 0; i < 100; i++ {
					input := uint64(uint32(i*17 - 33))
					want := tc.want(i)
					got, err := instance.Invoke("run", input)
					if err != nil || len(got) != 1 || got[0] != want {
						t.Fatalf("iteration %d = %v, %v; want [%d]", i, got, err, want)
					}
				}
				if err := instance.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if err := compiled.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestGCFreshStructRootStoreTrap(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcFreshStructLocalForwardBytes(false, false, 0))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if got, err := instance.Invoke("run", 1); err == nil {
		t.Fatalf("run = %v, want out-of-bounds trap", got)
	}
}
