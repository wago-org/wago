//go:build linux && amd64 && !tinygo

package wago

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

// Each of three invariant locals is first read inside a value-producing merge.
// Other reads resolve arrays loaded from fields, between accesses to the locals.
func gcLoopCertificatesModule(mergeOnly bool) []byte {
	return gcLoopCertificatesModuleN(mergeOnly, 3)
}

func gcLoopCertificatesModuleN(mergeOnly bool, objects byte) []byte {
	return gcLoopCertificatesModuleWithRefs(mergeOnly, objects, false)
}

func gcLoopCertificatesModuleWithRefs(mergeOnly bool, objects byte, refs bool) []byte {
	holder := byte(1)
	if refs {
		holder = 5
	}
	init := []byte{0}
	for i := byte(0); i < objects; i++ {
		init = append(init, 0x41, 7+i, 0xfb, 8, 0, 1, 0x41, 17+i, 0xfb, 0, 1)
		if refs {
			init = append(init, 0xfb, 8, 4, 1, 0x41, 1, 0xfb, 0, 5)
		}
		init = append(init, 0x24, i)
	}
	init = append(init, 0x0b)
	body := []byte{2, objects, 0x63, holder, 2, 0x7f}
	sum, iter := objects+1, objects+2
	for i := byte(0); i < objects; i++ {
		body = append(body, 0x23, i, 0x21, i+1)
	}
	body = append(body, 0x02, 0x40, 0x03, 0x40, 0x20, iter, 0x20, 0, 0x4f, 0x0d, 1)
	for i := byte(1); i <= objects; i++ {
		body = append(body, 0x20, sum, 0x20, iter, 0x45, 0x04, 0x7f, 0x41, 0, 0x05, 0x20, i, 0xfb, 2, holder, 1, 0x0b, 0x6a, 0x21, sum)
		if !mergeOnly {
			body = append(body, 0x20, sum, 0x20, i, 0xfb, 2, holder, 0, 0x41, 0)
			if refs {
				body = append(body, 0xfb, 11, 4, 0xd4, 0xfb, 2, 1, 1)
			} else {
				body = append(body, 0xfb, 11, 0)
			}
			body = append(body, 0x6a, 0x21, sum)
		}
	}
	body = append(body, 0x20, iter, 0x41, 1, 0x6a, 0x21, iter, 0x0c, 0, 0x0b, 0x0b, 0x20, sum, 0x0b)
	globals := [][]byte{}
	for range objects {
		globals = append(globals, []byte{0x63, holder, 1, 0xd0, holder, 0x0b})
	}
	types := [][]byte{[]byte{0x5e, 0x7f, 1}, []byte{0x5f, 2, 0x63, 0, 1, 0x7f, 1}, wasmtest.FuncType(nil, nil), wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})}
	if refs {
		types = append(types, []byte{0x5e, 0x63, 1, 1}, []byte{0x5f, 2, 0x63, 4, 1, 0x7f, 1})
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec([]byte{2}, []byte{3})),
		wasmtest.Section(6, wasmtest.Vec(globals...)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("init", 0, 0), wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(init))), init...), append(wasmtest.ULEB(uint32(len(body))), body...))))
}

func TestGCLoopCertificatesMergesAndLoadedArrays(t *testing.T) {
	for _, mergeOnly := range []bool{false, true} {
		c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcLoopCertificatesModule(mergeOnly))
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		for _, profile := range []GCProfile{GCProfileThroughput, GCProfileTiny} {
			in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{Profile: profile, NurseryBytes: 4096, VerifyAfterCollect: true}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = in.Invoke("init"); err != nil {
				t.Fatal(err)
			}
			for _, n := range []uint64{0, 1, 2, 9} {
				want := uint64(0)
				if n > 1 {
					want = (n - 1) * 54
				}
				if !mergeOnly {
					want += n * 24
				}
				got, err := in.Invoke("run", n)
				if err != nil || len(got) != 1 || got[0] != want {
					t.Fatalf("mergeOnly=%v profile=%v n=%d: got=%v err=%v want=%d", mergeOnly, profile, n, got, err, want)
				}
			}
			in.Close()
		}
	}
}

func TestGCLoopCertificateFirstDynamicNull(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcLoopCertificatesModule(true))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	// No init: every compact local is null. Iteration zero skips all reads.
	for _, n := range []uint64{0, 1} {
		got, err := in.Invoke("run", n)
		if err != nil || len(got) != 1 || got[0] != 0 {
			t.Fatalf("n=%d premature trap: %v %v", n, got, err)
		}
	}
	_, err = in.Invoke("run", 2)
	if tr, ok := err.(*TrapError); !ok || tr.Code != TrapNullReference {
		t.Fatalf("first dynamic read: got %T %v, want null-reference trap", err, err)
	}
}

func TestGCLoopCertificatesFourLocals(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcLoopCertificatesModuleN(false, 4))
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
	got, err := in.Invoke("run", 5)
	if err != nil || len(got) != 1 || got[0] != 5*34+4*74 {
		t.Fatalf("four locals: %v %v", got, err)
	}
}

func TestGCLoopCertificatesLoadedReferenceArray(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcLoopCertificatesModuleWithRefs(false, 3, true))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err = in.Invoke("init"); err != nil {
		t.Fatal(err)
	}
	for _, n := range []uint64{0, 1, 2, 7} {
		want := n * 54
		if n > 1 {
			want += (n - 1) * 3
		}
		got, err := in.Invoke("run", n)
		if err != nil || len(got) != 1 || got[0] != want {
			t.Fatalf("reference array n=%d: %v %v want%d", n, got, err, want)
		}
	}
}
