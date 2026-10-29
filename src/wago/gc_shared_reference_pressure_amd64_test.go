//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Final compact-reference fields exercise native reads while live numeric
// parameters, locals, and an exception handler put pressure on the registers.
func gcSharedReferencePressureModule() []byte {
	paramType := []byte{0x60, 0x0d, 0x63, 0x00}
	for range 12 {
		paramType = append(paramType, 0x7f)
	}
	paramType = append(paramType, 0x01, 0x7f)
	types := [][]byte{
		{0x5f, 0x01, 0x6d, 0x01},       // 0: final box (mut eqref)
		{0x50, 0x00, 0x5e, 0x7f, 0x01}, // 1: open (array (mut i32))
		{0x5f, 0x01, 0x63, 0x01, 0x01}, // 2: final box (mut (ref null array))
		paramType,
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil), // 5: tag payload
	}
	readRef := []byte{0x20, 0x00, 0xfb, 0x02, 0x00, 0x00}
	// Keep every parameter live after both reference reads. Their different
	// values expose a clobbered local even when the loaded references are valid.
	paramBody := []byte{0x01, 0x01, 0x7f}
	paramBody = append(paramBody, readRef...)
	paramBody = append(paramBody, readRef...)
	paramBody = append(paramBody, 0xfb, 0x16, 0x6c, 0xfb, 0x1d, 0x21, 0x0d,
		0xfb, 0x16, 0x6c, 0xfb, 0x1d, 0x20, 0x0d, 0x6a)
	for i := 1; i <= 12; i++ {
		paramBody = append(paramBody, 0x20, byte(i), 0x6a)
	}
	paramBody = append(paramBody, 0x0b)
	paramCode := append(wasmtest.ULEB(uint32(len(paramBody))), paramBody...)
	caller := []byte{0x41, 0x11, 0xfb, 0x1c, 0xfb, 0x00, 0x00}
	for i := 1; i <= 12; i++ {
		caller = append(caller, 0x41)
		caller = append(caller, wasmtest.SLEB32(int32(i*11))...)
	}
	caller = append(caller, 0x10, 0x00, 0x0b)
	// Twelve numeric locals, one parent and one typed array parent. The eqref
	// field is changed between reads while its parent's raw address may be reused.
	localBody := []byte{0x03, 0x0c, 0x7f, 0x01, 0x63, 0x00, 0x01, 0x63, 0x02}
	for i := 0; i < 12; i++ {
		localBody = append(localBody, 0x41)
		localBody = append(localBody, wasmtest.SLEB32(int32((i+1)*13))...)
		localBody = append(localBody, 0x21, byte(i))
	}
	localBody = append(localBody,
		0x41, 0x17, 0xfb, 0x1c, 0xfb, 0x00, 0x00, 0x21, 0x0c,
		0x41, 0x1f, 0x41, 0x02, 0xfb, 0x06, 0x01, 0xfb, 0x00, 0x02, 0x21, 0x0d,
		0x20, 0x0c, 0xfb, 0x02, 0x00, 0x00, 0xfb, 0x16, 0x6c, 0xfb, 0x1d,
		0x20, 0x0c, 0x41, 0x1d, 0xfb, 0x1c, 0xfb, 0x05, 0x00, 0x00,
		0x20, 0x0c, 0xfb, 0x02, 0x00, 0x00, 0xfb, 0x16, 0x6c, 0xfb, 0x1d, 0x6a,
		0x20, 0x0d, 0xfb, 0x02, 0x02, 0x00, 0xd4, 0x41, 0x01, 0xfb, 0x0b, 0x01, 0x6a,
	)
	for i := 0; i < 12; i++ {
		localBody = append(localBody, 0x20, byte(i), 0x6a)
	}
	localBody = append(localBody, 0x0b)
	localCode := append(wasmtest.ULEB(uint32(len(localBody))), localBody...)
	// Reference reads execute with an active exception handler. A cached raw
	// object address must not occupy the register that owns that handler chain.
	ehBody := []byte{0x02, 0x01, 0x63, 0x00, 0x01, 0x7f,
		0x41, 0x11, 0xfb, 0x1c, 0xfb, 0x00, 0x00, 0x21, 0x00,
		0x02, 0x7f, 0x1f, 0x40, 0x01, 0x00, 0x00, 0x00,
		0x20, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x1a,
		0x20, 0x00, 0xfb, 0x02, 0x00, 0x00, 0xfb, 0x16, 0x6c, 0xfb, 0x1d,
		0x08, 0x00, 0x0b, 0x41, 0x00, 0x0b, 0x21, 0x01,
		0x20, 0x00, 0xfb, 0x02, 0x00, 0x00, 0xfb, 0x16, 0x6c, 0xfb, 0x1d,
		0x20, 0x01, 0x6a, 0x0b,
	}
	ehCode := append(wasmtest.ULEB(uint32(len(ehBody))), ehBody...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(3), wasmtest.ULEB(4), wasmtest.ULEB(4), wasmtest.ULEB(4))),
		wasmtest.Section(13, wasmtest.Vec([]byte{0x00, 0x05})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("params", 0, 1), wasmtest.ExportEntry("locals", 0, 2), wasmtest.ExportEntry("eh", 0, 3))),
		wasmtest.Section(10, wasmtest.Vec(paramCode, wasmtest.Code(caller), localCode, ehCode)),
	)
}

func TestGCSharedReferenceReadPreservesPressureValues(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcSharedReferencePressureModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()

	for _, code := range []*Compiled{compiled} {
		for _, cfg := range []GCConfig{
			{StressNurseryBytes: 256, CollectEveryAlloc: true, VerifyAfterCollect: true},
			{Profile: GCProfileTiny, TinyHeapBytes: 512, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true},
		} {
			in, err := Instantiate(code, InstantiateOptions{GC: cfg})
			if err != nil {
				t.Fatal(err)
			}
			if !in.gc.CollectsOnAllocation() {
				in.Close()
				t.Fatal("pressure fixture requires active collection")
			}
			for range 8 {
				for _, test := range []struct {
					name string
					want uint64
				}{{"params", 34 + 11*78}, {"locals", 23 + 29 + 31 + 13*78}, {"eh", 34}} {
					got, err := in.Invoke(test.name)
					if err != nil || len(got) != 1 || got[0] != test.want {
						in.Close()
						t.Fatalf("%s with profile %d = %v, %v; want [%d]", test.name, cfg.Profile, got, err, test.want)
					}
				}
			}
			in.Close()
		}
	}
}

// This reproduces the decoder's adjacent tuple reads before several reference
// constructors. The large offset must never replace the separately rooted child.
func gcDecoderTupleReferenceModule(extraLocals uint32) []byte {
	types := [][]byte{
		{0x50, 0x00, 0x5f, 0x01, 0x7f, 0x01},                                                       // open base 0
		{0x4f, 0x01, 0x00, 0x5f, 0x01, 0x7f, 0x01},                                                 // child 1 <: base
		{0x5f, 0x02, 0x63, 0x00, 0x01, 0x7f, 0x01},                                                 // tuple 2: ref, offset
		{0x4f, 0x01, 0x00, 0x5f, 0x02, 0x7f, 0x01, 0x63, 0x02, 0x01},                               // wrapper 3
		{0x5e, 0x63, 0x00, 0x01},                                                                   // array 4: base refs
		{0x5f, 0x02, 0x7f, 0x01, 0x63, 0x04, 0x01},                                                 // array wrapper 5
		{0x4f, 0x01, 0x00, 0x5f, 0x04, 0x7f, 0x01, 0x63, 0x00, 0x01, 0x7f, 0x01, 0x63, 0x05, 0x01}, // aggregate 6
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	}
	body := []byte{0x05, 0x09, 0x7f, 0x01, 0x63, 0x02, 0x01, 0x63, 0x00, 0x01, 0x7f}
	body = append(body, wasmtest.ULEB(extraLocals+1)...)
	body = append(body, 0x63, 0x06)
	for i := 0; i < 9; i++ {
		body = append(body, 0x41)
		body = append(body, wasmtest.SLEB32(int32((i+1)*17))...)
		body = append(body, 0x21, byte(i))
	}
	body = append(body, 0x41, 0x01, 0x41)
	body = append(body, wasmtest.SLEB32(91)...)
	body = append(body, 0xfb, 0x00, 0x01, 0x41)
	body = append(body, wasmtest.SLEB32(0x4e96ea)...)
	body = append(body,
		0xfb, 0x00, 0x02, 0xfb, 0x00, 0x03,
		0xfb, 0x02, 0x03, 0x01, 0x22, 0x09, // unwrap; local.tee tuple
		0xfb, 0x02, 0x02, 0x00, 0x21, 0x0a, // tuple.ref -> local 10
		0x20, 0x09, 0xfb, 0x02, 0x02, 0x01, 0x21, 0x0b, // tuple.offset -> local 11
		0x41, 0x01, 0x20, 0x0a, 0x20, 0x0b, 0x41, 0x00,
		0xfb, 0x08, 0x04, 0x00, 0xfb, 0x00, 0x05,
		0xfb, 0x00, 0x06, 0x22, 0x0c,
		0xfb, 0x02, 0x06, 0x01, 0xfb, 0x02, 0x00, 0x00,
		0x41,
	)
	body = append(body, wasmtest.SLEB32(91)...)
	body = append(body, 0x46, 0x20, 0x0c, 0xfb, 0x02, 0x06, 0x02, 0x41)
	body = append(body, wasmtest.SLEB32(0x4e96ea)...)
	body = append(body, 0x46, 0x71, 0x0b)
	code := append(wasmtest.ULEB(uint32(len(body))), body...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(7), wasmtest.ULEB(7))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(code, code)),
	)
}

func TestGCDecoderTupleReferenceSurvivesAdjacentScalarRead(t *testing.T) {
	for _, extra := range []uint32{0, 64} {
		compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcDecoderTupleReferenceModule(extra))
		if err != nil {
			t.Fatal(err)
		}

		for _, code := range []*Compiled{compiled} {
			for _, cfg := range []GCConfig{
				{StressNurseryBytes: 256, CollectEveryAlloc: true, VerifyAfterCollect: true},
				{Profile: GCProfileTiny, TinyHeapBytes: 512, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true},
			} {
				in, err := Instantiate(code, InstantiateOptions{GC: cfg})
				if err != nil {
					t.Fatal(err)
				}
				if !in.gc.CollectsOnAllocation() {
					in.Close()
					t.Fatal("decoder tuple fixture requires active collection")
				}
				for range 8 {
					got, err := in.Invoke("run")
					if err != nil || len(got) != 1 || got[0] != 1 {
						in.Close()
						t.Fatalf("decoder tuple extra=%d profile=%d: %v, %v; want [1]", extra, cfg.Profile, got, err)
					}
				}
				in.Close()
			}
		}
		compiled.Close()
		compiled.Close()
	}
}
