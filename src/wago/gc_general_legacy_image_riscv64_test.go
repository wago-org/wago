//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"testing"

	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestRV64GeneralGCImageMayMatchLegacyCode(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source func(testing.TB) []byte
		legacy func(*Compiled) bool
		invoke string
	}{
		{"fill", rv64PublicGCArrayFillBytes, rv64GCArrayFillExecutionCode, "array_get_nth"},
		{"init-data", rv64PublicGCArrayInitDataBytes, rv64GCArrayInitDataExecutionCode, "array_get_nth"},
		{"struct-basic", rv64PublicGCStructBasicBytes, nil, "get_0_0"},
		{"reference-array", rv64PublicGCArrayReferenceBytes, nil, "len"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Custom metadata is semantically inert and cannot select optimization.
			source := append(tc.source(t), wasmtest.Section(0, wasmtest.Name("inert"))...)
			c, e := NewRuntimeConfig().Compile(source)
			if e != nil {
				t.Fatal(e)
			}
			defer c.Close()
			if !rv64FreshRootedGC(c) || c.genericGCFrameRoots() == nil || !rv64SourceContractValid(c) {
				t.Fatal("general image lacks exact rooted execution proof")
			}
			if tc.legacy != nil && !tc.legacy(c) {
				t.Fatal("fixture no longer exercises native byte collision")
			}

			if e != nil {
				t.Fatal(e)
			}

			if e != nil {
				t.Fatal(e)
			}

			for _, candidate := range []*Compiled{c} {
				in, e := Instantiate(candidate, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, VerifyAfterCollect: true}})
				if e != nil {
					t.Fatal(e)
				}
				got, e := in.Invoke(tc.invoke, make([]uint64, len(candidate.Funcs[candidate.Exports[tc.invoke]].Params))...)
				in.Close()
				want := uint64(0)
				if tc.name == "reference-array" {
					want = 2
				}
				if e != nil || len(got) != 1 || got[0] != want {
					t.Fatalf("general image execution=%v,%v", got, e)
				}
			}
		})
	}
}
