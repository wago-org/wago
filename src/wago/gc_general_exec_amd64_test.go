//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"encoding/hex"
	"testing"
)

const gcHelperCallerSavedLocalsHex = "0061736d010000000114045e77015e63000160037f7f7f0164006000017f03030202030503010001060a016401004104fb07010b0707010372756e00010c01010a3e022f0201630001640023002000fb0b012203d14504402003d40f0b20012002fb0900002104230020002004fb0e0120040b0c004100410241031000fb0f0b0b1d01011a6162636465666768696a6b6c6d6e6f707172737475767778797a0028046e616d65010401000166040b020001610105636163686507080100056361636865090401000164"

const gcSubtypeStructAccessHex = "0061736d0100000001140350005f017f015001005f027f017f006000017f0303020202071102036765740000077365745f67657400010a2c020d0041074108fb0001fb0200000b1c0101640141014102fb0001210020004109fb0500002000fb0201000b0015046e616d65040e0200046261736501056368696c64"

func compileGCGeneralFixture(t testing.TB, encoded string) *Compiled {
	t.Helper()
	data, err := hex.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit), data)
	if err != nil {
		t.Fatal(err)
	}
	return compiled
}

func TestGCHelperCallPreservesCallerSavedPinnedLocals(t *testing.T) {
	compiled := compileGCGeneralFixture(t, gcHelperCallerSavedLocalsHex)
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	got, err := instance.Invoke("run")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != 3 {
		t.Fatalf("run = %v, want [3]", got)
	}
}

func TestGCStructAccessAcceptsRuntimeSubtype(t *testing.T) {
	compiled := compileGCGeneralFixture(t, gcSubtypeStructAccessHex)
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	for _, tc := range []struct {
		name string
		want uint64
	}{
		{name: "get", want: 7},
		{name: "set_get", want: 9},
	} {
		got, err := instance.Invoke(tc.name)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if len(got) != 1 || got[0] != tc.want {
			t.Fatalf("%s = %v, want [%d]", tc.name, got, tc.want)
		}
	}
}

func BenchmarkGCStructSubtypeSetGet(b *testing.B) {
	compiled := compileGCGeneralFixture(b, gcSubtypeStructAccessHex)
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{ThroughputHeapBytes: 256 << 20}})
	if err != nil {
		b.Fatal(err)
	}
	defer instance.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, err := instance.Invoke("set_get")
		if err != nil {
			b.Fatal(err)
		}
		if len(got) != 1 || got[0] != 9 {
			b.Fatalf("set_get = %v, want [9]", got)
		}
	}
}
