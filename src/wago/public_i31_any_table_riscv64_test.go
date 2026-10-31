//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"encoding/binary"
	"slices"
	"strings"
	"testing"
)

func rv64I31OfficialAnyTableBytes(t testing.TB) []byte {
	return stagedGCI31AnyTableBytes(t)
}

func rv64I31OfficialAnyTableCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()

	original, err := rv64I31Config().Compile(rv64I31OfficialAnyTableBytes(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = original.Close() })
	c := original

	return c
}

func TestRV64PublicI31OfficialAnyTableExecutionAndArtifact(t *testing.T) {
	data := rv64I31OfficialAnyTableBytes(t)
	wantSource := [sha256.Size]byte{0x57, 0x23, 0x87, 0xa2, 0xc9, 0xd7, 0xea, 0x91, 0x12, 0xf3, 0x94, 0x00, 0x25, 0xb7, 0xc5, 0x70, 0x41, 0xcd, 0x94, 0x78, 0x18, 0x5e, 0xd7, 0xe3, 0x2b, 0xb9, 0x3a, 0x01, 0xfb, 0xfa, 0x5a, 0x69}
	if len(data) != 262 || sha256.Sum256(data) != wantSource {
		t.Fatal("official i31 anyref-table source identity changed")
	}
	for _, artifact := range []bool{false} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			c := rv64I31OfficialAnyTableCompile(t, artifact)
			view := c.executionView()
			wantProduct, wantStaged := stagedGCI31ProductAnyTable, CoreFeatureTypedFunctionReferences|CoreFeatureGC
			if artifact {
				wantProduct, wantStaged = stagedGCI31ProductCore, CoreFeatureGC
			}
			if !rv64I31OfficialAnyTable(view) || !rv64I31Core(view) || railshotValidateCompiled(view) != nil || c.stagedGCI31Product() != wantProduct || c.stagedFeatures() != wantStaged {
				t.Fatal("missing exact retained official i31 anyref-table witness")
			}
			rt := NewRuntime(WithRuntimeConfig(rv64I31Config()))
			defer rt.Close()
			in := rv64NativeTokenInstantiate(t, rt, c, NewImports())
			defer in.Close()
			if in.gc != nil {
				t.Fatal("non-allocating official i31 anyref-table provisioned a collector")
			}
			_ = c.Close()

			invoke := func(name string, want []uint64, args ...uint64) {
				t.Helper()
				got, err := in.Invoke(name, args...)
				if err != nil || !slices.Equal(got, want) {
					t.Fatalf("%s%v=%v err=%v want=%v", name, args, got, err, want)
				}
			}
			invoke("size", []uint64{3})
			invoke("get", []uint64{999}, 0xcafe123400000000)
			invoke("get", []uint64{888}, 1)
			invoke("get", []uint64{777}, 2)

			desc := in.tableDescriptor(0)
			binary.LittleEndian.PutUint64(desc[8:16], 0)
			if _, err := in.Invoke("get", 0); err == nil {
				t.Fatal("null anyref table cast succeeded")
			} else if trap, ok := err.(*TrapError); !ok || trap.Code != TrapNullReference {
				t.Fatalf("null anyref table trap=%v, want %v", err, TrapNullReference)
			}
			binary.LittleEndian.PutUint64(desc[8:16], 2)
			if _, err := in.Invoke("get", 0); err == nil {
				t.Fatal("non-i31 anyref table cast succeeded")
			} else if trap, ok := err.(*TrapError); !ok || trap.Code != TrapCastFailure {
				t.Fatalf("non-i31 anyref table trap=%v, want %v", err, TrapCastFailure)
			}
			binary.LittleEndian.PutUint64(desc[8:16], 1999)

			invoke("grow", []uint64{3}, 0xdeadbeef00000002, 0xcafe12340000014d)
			invoke("size", []uint64{5})
			invoke("get", []uint64{333}, 3)
			invoke("get", []uint64{333}, 4)
			invoke("fill", nil, 0x1234567800000002, 0xabcdef010000006f, 0xfeedface00000002)
			invoke("get", []uint64{111}, 2)
			invoke("get", []uint64{111}, 3)
			invoke("copy", nil, 0x9999999900000003, 0xaaaaaaaa00000000, 0xbbbbbbbb00000002)
			invoke("get", []uint64{999}, 3)
			invoke("get", []uint64{888}, 4)
			invoke("init", nil, 0xcccccccc00000001, 0xdddddddd00000000, 0xeeeeeeee00000003)
			invoke("get", []uint64{123}, 1)
			invoke("get", []uint64{456}, 2)
			invoke("get", []uint64{789}, 3)
			invoke("grow", []uint64{uint64(^uint32(0))}, 6, 44)
			invoke("size", []uint64{5})
			for name, args := range map[string][]uint64{
				"fill": {4, 55, 2},
				"copy": {4, 0, 2},
				"init": {4, 0, 2},
			} {
				if _, err := in.Invoke(name, args...); err == nil || !strings.Contains(err.Error(), "table") {
					t.Fatalf("out-of-bounds table.%s trap=%v", name, err)
				}
				invoke("get", []uint64{888}, 4)
			}
			if _, err := in.Invoke("get", I32(5)); err == nil || !strings.Contains(err.Error(), "table") {
				t.Fatalf("out-of-bounds table.get trap=%v", err)
			}
		})
	}
}
