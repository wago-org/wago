//go:build linux && amd64

package wago

import "testing"

func FuzzSnapshotV4GCGraphMutations(f *testing.F) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit), gcLiveSnapshotCycleModule())
	if err != nil {
		f.Fatal(err)
	}
	snapshot, err := Capture(compiled, SnapshotOptions{Kind: SnapshotWarm, WarmFunc: "warm", WarmArgs: []uint64{7}})
	if err != nil {
		compiled.Close()
		f.Fatal(err)
	}
	seed, err := snapshot.MarshalBinary()
	compiled.Close()
	if err != nil {
		f.Fatal(err)
	}
	f.Add(seed)
	for _, cut := range []int{0, 1, 4, 5, len(seed) / 2, len(seed) - 1} {
		if cut >= 0 && cut <= len(seed) {
			f.Add(append([]byte(nil), seed[:cut]...))
		}
	}
	for _, pos := range []int{4, len(seed) / 2, len(seed) - 1} {
		if pos >= 0 && pos < len(seed) {
			mutated := append([]byte(nil), seed...)
			mutated[pos] ^= 0xff
			f.Add(mutated)
		}
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		loaded, err := LoadSnapshot(data)
		if err != nil {
			return
		}
		defer loaded.Module().Close()
		encoded, err := loaded.MarshalBinary()
		if err != nil {
			t.Fatalf("MarshalBinary after successful load: %v", err)
		}
		roundtrip, err := LoadSnapshot(encoded)
		if err != nil {
			t.Fatalf("LoadSnapshot after canonical re-encode: %v", err)
		}
		defer roundtrip.Module().Close()
		in, err := Instantiate(roundtrip)
		if err != nil {
			t.Fatalf("Instantiate validated snapshot: %v", err)
		}
		if err := in.Close(); err != nil {
			t.Fatalf("close restored instance: %v", err)
		}
	})
}
