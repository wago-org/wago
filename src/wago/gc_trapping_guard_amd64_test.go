//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"encoding/binary"
	"errors"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"os"
	"strings"
	"testing"
	"unsafe"
)

func TestGCTrappingGuard(t *testing.T) {
	for _, fixture := range []string{"gc-trapping-guard", "gc-trapping-guard-direct"} {
		t.Run(fixture, func(t *testing.T) { testGCTrappingGuardFixture(t, fixture) })
	}
}

func testGCTrappingGuardFixture(t *testing.T, fixture string) {
	data, err := os.ReadFile("../../tests/fixtures/wasm/" + fixture + ".wasm")
	if err != nil {
		t.Fatal(err)
	}
	for _, inline := range []bool{true, false} {
		c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("inline", inline), data)
		if err != nil {
			t.Fatal(err)
		}
		for _, cfg := range []GCConfig{{VerifyAfterCollect: true}, {CollectEveryAlloc: true, StressNurseryBytes: 128, VerifyAfterCollect: true}, {Profile: GCProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16, VerifyAfterCollect: true}} {
			in, err := Instantiate(c, InstantiateOptions{GC: cfg})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = in.Invoke("init"); err != nil {
				t.Fatal(err)
			}
			for _, tc := range []struct {
				name string
				want uint64
			}{{"same-field", 4}, {"indexed-block", 4}, {"packed-s", 0xfffffffe}, {"packed-u", 254}} {
				got, e := in.Invoke(tc.name, 1)
				if e != nil || len(got) != 1 || got[0] != tc.want {
					t.Fatalf("%s=%v,%v want%d", tc.name, got, e, tc.want)
				}
			}
			if got, e := in.Invoke("pressure", 1, 1, 2, 3, 4, 5, 6, 7, 8); e != nil || len(got) != 1 || got[0] != 36 {
				t.Fatalf("pressure=%v,%v want36", got, e)
			}
			for _, name := range []string{"loop", "negative-loop", "noninline-loop"} {
				if got, e := in.Invoke(name); e != nil || len(got) != 1 || got[0] != 8192*17 {
					t.Fatalf("%s=%v,%v want139264", name, got, e)
				}
			}

			for _, setup := range []string{"init", "clear", "wrong"} {
				if _, err = in.Invoke(setup); err != nil {
					t.Fatal(err)
				}
				names := []string{"read", "nullable", "roots", "dirty"}
				if fixture == "gc-trapping-guard-direct" {
					names = append(names, "roots-call")
				}
				for _, name := range names {
					for _, index := range []uint64{1, 4} {
						before := in.gc.Stats()
						got, err := in.Invoke(name, index|uint64(1)<<40)
						if setup == "init" && index == 1 {
							if err != nil || len(got) != 1 {
								t.Fatalf("%s=%v,%v", name, got, err)
							}
							if strings.HasPrefix(name, "roots") && cfg.CollectEveryAlloc {
								after := in.gc.Stats()
								if after.FullCollections+after.MinorCollections <= before.FullCollections+before.MinorCollections {
									t.Fatal("root probe did not collect")
								}
							}
							if strings.HasPrefix(name, "roots") && got[0] != 17 {
								t.Fatalf("root result=%v", got)
							}
							continue
						}
						want := "cast failure"
						if setup == "init" {
							want = "unreachable"
						}
						if setup == "clear" && name == "nullable" {
							want = "null reference"
						}
						if err == nil || !strings.Contains(err.Error(), want) {
							t.Fatalf("%s/%s/%d err=%v want%q", setup, name, index, err, want)
						}
					}
				}
			}
			if got, e := in.Invoke("dirty-value"); e != nil || got[0] != 6 {
				t.Fatalf("dirty cold exits=%v,%v want6", got, e)
			}
			if err = in.CollectGC(); err != nil {
				t.Fatal(err)
			}
			in.Close()
		}
		c.Close()
	}
}

func BenchmarkGCTrappingGuard(b *testing.B) {
	data, err := os.ReadFile("../../tests/fixtures/wasm/gc-trapping-guard.wasm")
	if err != nil {
		b.Fatal(err)
	}
	for _, name := range []string{"loop", "same-field", "negative-loop", "noninline-loop", "pressure"} {
		b.Run(name, func(b *testing.B) {
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
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
			var args []uint64
			want := uint64(8192 * 17)
			if name == "same-field" {
				args = []uint64{1}
				want = 4
			}
			if name == "pressure" {
				args = []uint64{1, 1, 2, 3, 4, 5, 6, 7, 8}
				want = 36
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				got, e := in.Invoke(name, args...)
				if e != nil || len(got) != 1 || got[0] != want {
					b.Fatalf("%s=%v,%v want%d", name, got, e, want)
				}
			}
		})
	}
}

// The first full access must fail before the comparison's explicit trap when
// the physical extent cannot hold the count field. Restore the header before
// teardown so this diagnostic does not corrupt later collector checks.
func TestGCTrappingGuardFirstExtent(t *testing.T) {
	for _, fixture := range []string{"gc-trapping-guard", "gc-trapping-guard-direct"} {
		t.Run(fixture, func(t *testing.T) { testGCTrappingGuardFirstExtentFixture(t, fixture) })
	}
}

func testGCTrappingGuardFirstExtentFixture(t *testing.T, fixture string) {
	data, err := os.ReadFile("../../tests/fixtures/wasm/" + fixture + ".wasm")
	if err != nil {
		t.Fatal(err)
	}
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err = in.Invoke("init"); err != nil {
		t.Fatal(err)
	}
	ref := uint64(uint32(readGlobalObject(in.globalCells[0], in.c.Globals[0].Type)))
	view := in.gc.NativeView()
	handle := unsafe.Slice((*byte)(offHeapPtr(view.Handles+uintptr(ref>>1)*gc.NativeHandleStride)), gc.NativeHandleStride)
	header := unsafe.Slice((*byte)(offHeapPtr(view.Spaces[handle[gc.NativeHandleSpaceOffset]].Base+uintptr(binary.LittleEndian.Uint32(handle)))), gc.HeaderSize)
	old := binary.LittleEndian.Uint32(header[4:])
	binary.LittleEndian.PutUint32(header[4:], gc.PayloadOffset+4)
	defer binary.LittleEndian.PutUint32(header[4:], old)
	_, err = in.Invoke("dirty", 4)
	var trap *TrapError
	if !errors.As(err, &trap) || trap.Code != TrapCastFailure {
		t.Fatalf("truncated first read=%v want cast failure before unreachable", err)
	}
	if got, e := in.Invoke("dirty-value"); e != nil || len(got) != 1 || got[0] != 1 {
		t.Fatalf("dirty global after first extent trap=%v,%v", got, e)
	}
	// A later larger field must retain its later trap. In particular, a true
	// guard still traps as unreachable before that larger extent is checked.
	binary.LittleEndian.PutUint32(header[4:], gc.PayloadOffset+8)
	for _, tc := range []struct {
		index uint64
		code  TrapCode
	}{{1, TrapCastFailure}, {4, TrapUnreachable}} {
		_, err = in.Invoke("larger", tc.index)
		if !errors.As(err, &trap) || trap.Code != tc.code {
			t.Fatalf("larger index%d=%v want trap%d", tc.index, err, tc.code)
		}
	}
}

func TestGCTrappingGuardDirectComparisons(t *testing.T) {
	data, err := os.ReadFile("../../tests/fixtures/wasm/gc-trapping-guard-direct.wasm")
	if err != nil {
		t.Fatal(err)
	}
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err = in.Invoke("init"); err != nil {
		t.Fatal(err)
	}
	expected, err := in.Invoke("read", 1)
	if err != nil || len(expected) != 1 {
		t.Fatalf("reference result=%v,%v", expected, err)
	}
	cases := []struct {
		op    string
		traps [4]bool
	}{
		{"eq", [4]bool{false, true, false, false}},
		{"ne", [4]bool{true, false, true, true}},
		{"lt_s", [4]bool{true, false, false, true}},
		{"lt_u", [4]bool{true, false, false, false}},
		{"gt_s", [4]bool{false, false, true, false}},
		{"gt_u", [4]bool{false, false, true, true}},
		{"le_s", [4]bool{true, true, false, true}},
		{"le_u", [4]bool{true, true, false, false}},
		{"ge_s", [4]bool{false, true, true, false}},
		{"ge_u", [4]bool{false, true, true, true}},
	}
	for _, tc := range cases {
		for i, index := range []uint64{3, 4, 5, 0xffffffff} {
			for _, upper := range []uint64{0, 1 << 40} {
				got, err := in.Invoke("cmp-"+tc.op, index|upper)
				if tc.traps[i] {
					var trap *TrapError
					if !errors.As(err, &trap) || trap.Code != TrapUnreachable {
						t.Errorf("%s(%#x)=%v,%v; want unreachable", tc.op, index|upper, got, err)
					}
				} else if err != nil || len(got) != 1 || got[0] != expected[0] {
					t.Errorf("%s(%#x)=%v,%v; want %v", tc.op, index|upper, got, err, expected)
				}
			}
		}
	}
}
