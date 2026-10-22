//go:build linux && riscv64 && !tinygo && !wago_guardpage

package wago

import (
	"context"
	"fmt"
	railshotshared "github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"reflect"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

var gcFrameRootPlanSink *railshotshared.GCModuleFrameRootPlan

func TestRV64OwnedFunctionRootOffsetAdmission(t *testing.T) {
	flag := railshotshared.GCFrameFuncRefRootFlag
	valid := []uint32{8, flag | 16}
	if validGCFrameOffsets(valid, 32) {
		t.Fatal("ordinary GC ABI accepted raw function roots")
	}
	if !validGCFrameOffsetsMode(valid, 32, true) {
		t.Fatal("owned Env ABI rejected exact function root")
	}
	for _, offsets := range [][]uint32{{8, flag | 8}, {flag | 17}, {flag | 32}, {flag | 16, 8}} {
		if validGCFrameOffsetsMode(offsets, 32, true) {
			t.Fatalf("invalid function offsets admitted: %v", offsets)
		}
	}
}

var rv64ExceptionNullHeaps = [...]byte{byte(wasm.HeapAny), byte(wasm.HeapFunc), byte(wasm.HeapExn), byte(wasm.HeapExtern), byte(wasm.HeapNone), byte(wasm.HeapNoFunc), byte(wasm.HeapNoExn), byte(wasm.HeapNoExtern), 0}

func rv64ExceptionMixedNullModule(globals, vector bool) []byte {
	types := [][]byte{{0x60, 0, 0}}
	var functions, exports, codes, cells [][]byte
	for i, heap := range rv64ExceptionNullHeaps {
		types = append(types, []byte{0x60, 0, 1, 0x63, heap})
		functions = append(functions, wasmtest.ULEB(uint32(i+1)))
		name := string([]byte{'a' + byte(i)})
		exports = append(exports, wasmtest.ExportEntry(name, 0, uint32(i)))
		body := []byte{0xd0, heap}
		if globals {
			body = []byte{0x23, byte(i)}
			cells = append(cells, []byte{0x63, heap, 0, 0xd0, heap, 0x0b})
		}
		if vector {
			body = append(body, 0xfd, 12)
			body = append(body, make([]byte, 16)...)
			body = append(body, 0x1a)
		}
		codes = append(codes, wasmtest.Code(append(body, 0x0b)))
	}
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(3, wasmtest.Vec(functions...))}
	if globals {
		sections = append(sections, wasmtest.Section(6, wasmtest.Vec(cells...)))
	}
	sections = append(sections, wasmtest.Section(7, wasmtest.Vec(exports...)), wasmtest.Section(10, wasmtest.Vec(codes...)))
	return wasmtest.Module(sections...)
}

func TestRV64ExceptionMixedNullDeclarations(t *testing.T) {
	for _, globals := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			t.Run(map[bool]string{false: "literal", true: "global"}[globals]+map[bool]string{false: "/scalar", true: "/vector"}[vector], func(t *testing.T) {
				cfg := rv64I31Config().WithCoreFeatures(platformCoreFeatures() | CoreFeatureGC | CoreFeatureTypedFunctionReferences)
				features := cfg.frontendFeatures()
				features.ExceptionHandling, features.ExceptionReferences = true, true
				c, err := compileWithFrontendFeatures(cfg, rv64ExceptionMixedNullModule(globals, vector), features)
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				if !rv64FreshRootedGC(c) {
					t.Fatalf("mixed null source lacks exact retained root/type proof: slots=%d owned=%v roots=%+v", c.syncHostSlots, c.needsOwnedExceptionReferences(), c.genericGCFrameRoots())
				}
				blob, err := c.MarshalBinary()
				if err != nil {
					t.Fatal(err)
				}
				var loaded Compiled
				if err := loaded.UnmarshalBinary(blob); err != nil {
					t.Fatal(err)
				}
				defer loaded.Close()
				if !globals && !vector {
					for _, change := range []struct {
						name   string
						mutate func(*Compiled)
					}{
						{"owned mode", func(c *Compiled) { c.setOwnedExceptionReferences(false) }},
						{"native ABI", func(c *Compiled) { c.codeCache.setNativeGCABIVersion(0) }},
						{"exact roots", func(c *Compiled) { c.validateMemo.gcFrameRoots = nil }},
						{"context header", func(c *Compiled) { c.needsFuncRefContextHeader = false }},
					} {
						t.Run("missing "+change.name, func(t *testing.T) {
							candidate := cloneCompiledMetadata(&loaded)
							indexes := compiledCacheIndexes{railshotTail: loaded.loadCompileIndexes().railshotTail}
							candidate.validateMemo = &validateMemo{gcFrameRoots: loaded.genericGCFrameRoots(), compileIndexes: &indexes}
							candidate.codeCache = &compiledCodeCache{gcMetadataFlags: loaded.codeCache.gcMetadataFlags}
							change.mutate(candidate)
							if err := candidate.validate(); err == nil {
								t.Fatal("incomplete owned EH proof admitted native execution")
							}
						})
					}
				}
				for _, compiled := range []*Compiled{c, &loaded} {
					in, err := instantiateCore(compiled, InstantiateOptions{})
					if err != nil {
						t.Fatal(err)
					}
					for i := range rv64ExceptionNullHeaps {
						name := string([]byte{'a' + byte(i)})
						got, err := in.Invoke(name)
						if err != nil || len(got) != 1 || got[0] != 0 {
							t.Fatalf("heap %x: result=%v error=%v", rv64ExceptionNullHeaps[i], got, err)
						}
					}
					if err := in.Close(); err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}

func TestRV64ExceptionReturnedValueSurvivesCaptureAndCollection(t *testing.T) {
	// capture returns an owned exception. The second capture reuses its native
	// frame and collects, while the caller must retain the first value.
	capture := []byte{0x02, 0x63, 0x69, 0x1f, 0x40, 1, 3, 0, 0x20, 0, 0x08, 0, 0x0b, 0x00, 0x0b, 0x0b}
	run := []byte{1, 1, 0x69, 0x41, 42, 0x10, 0, 0x21, 0, 0x41, 0xe3, 0, 0x10, 0, 0x1a,
		0x02, 0x7f, 0x1f, 0x7f, 1, 0, 0, 0, 0x20, 0, 0x0a, 0x41, 0, 0x0b, 0x0b, 0x0b}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil), []byte{0x60, 1, 0x7f, 1, 0x63, 0x69}, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2})),
		wasmtest.Section(13, wasmtest.Vec([]byte{0, 0})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(capture), append(wasmtest.ULEB(uint32(len(run))), run...))),
	)
	cfg := rv64I31Config()
	features := cfg.frontendFeatures()
	features.ExceptionHandling, features.ExceptionReferences = true, true
	c, err := compileWithFrontendFeatures(cfg, data, features)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	blob, err := c.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	var loaded Compiled
	if err := loaded.UnmarshalBinary(blob); err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	for _, compiled := range []*Compiled{c, &loaded} {
		in, err := instantiateCore(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, VerifyAfterCollect: true, StressNurseryBytes: 64}})
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 32; i++ {
			got, err := in.Invoke("run")
			if err != nil || len(got) != 1 || got[0] != 42 {
				t.Fatalf("round %d: result=%v error=%v", i, got, err)
			}
		}
		if err := in.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRV64ExceptionStateSurvivesCallsAndCollection(t *testing.T) {
	for _, table := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			t.Run(map[bool]string{false: "global", true: "table"}[table]+map[bool]string{false: "/scalar", true: "/vector"}[vector], func(t *testing.T) {
				capture := []byte{0x02, 0x69, 0x1f, 0x40, 1, 3, 0, 0x20, 0, 0x08, 0, 0x0b, 0x00, 0x0b, 0x0b}
				save, get := []byte{0x20, 0, 0x10, 0, 0x24, 0, 0x0b}, []byte{0x23, 0}
				state := wasmtest.Section(6, wasmtest.Vec([]byte{0x69, 1, 0xd0, 0x69, 0x0b}))
				if table {
					state = wasmtest.Section(4, wasmtest.Vec([]byte{0x69, 1, 1, 2}))
					save, get = []byte{0x41, 0, 0x20, 0, 0x10, 0, 0x26, 0, 0x0b}, []byte{0x41, 0, 0x25, 0}
				}
				var beforeTag, afterTag []byte
				if table {
					beforeTag = state
				} else {
					afterTag = state
				}
				read := append([]byte{0x02, 0x7f, 0x1f, 0x7f, 1, 0, 0, 0}, get...)
				read = append(read, 0x0a, 0x41, 0, 0x0b, 0x0b, 0x0b)
				if vector {
					prefix := append([]byte{0xfd, 0x0c}, make([]byte, 16)...)
					read = append(append(prefix, 0x1a), read...)
				}
				data := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil), []byte{0x60, 1, 0x7f, 1, 0x63, 0x69}, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, nil))),
					wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{0}, []byte{2}, []byte{3})), beforeTag,
					wasmtest.Section(13, wasmtest.Vec([]byte{0, 0})), afterTag, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("save", 0, 1), wasmtest.ExportEntry("read", 0, 2), wasmtest.ExportEntry("churn", 0, 3))),
					wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(capture), wasmtest.Code(save), wasmtest.Code(read), wasmtest.Code([]byte{0x41, 0xe3, 0, 0x10, 0, 0x1a, 0x0b}))))
				cfg := rv64I31Config()
				features := cfg.frontendFeatures()
				features.ExceptionHandling, features.ExceptionReferences = true, true
				c, err := compileWithFrontendFeatures(cfg, data, features)
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				blob, err := c.MarshalBinary()
				if err != nil {
					t.Fatal(err)
				}
				var loaded Compiled
				if err := loaded.UnmarshalBinary(blob); err != nil {
					t.Fatal(err)
				}
				defer loaded.Close()
				for _, compiled := range []*Compiled{c, &loaded} {
					in, err := instantiateCore(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
					if err != nil {
						t.Fatal(err)
					}
					if _, err := in.Invoke("save", 42); err != nil {
						t.Fatal(err)
					}
					for i := 0; i < 32; i++ {
						if _, err := in.Invoke("churn"); err != nil {
							t.Fatal(err)
						}
						if got, err := in.Invoke("read"); err != nil || len(got) != 1 || got[0] != 42 {
							t.Fatalf("round %d: result=%v error=%v", i, got, err)
						}
					}
					if err := in.Close(); err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}

func TestRV64ExceptionStateOnlyArtifact(t *testing.T) {
	for _, table := range []bool{false, true} {
		state, kind := wasmtest.Section(6, wasmtest.Vec([]byte{0x69, 1, 0xd0, 0x69, 0x0b})), byte(3)
		if table {
			state, kind = wasmtest.Section(4, wasmtest.Vec([]byte{0x69, 1, 1, 2})), 1
		}
		data := wasmtest.Module(state, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("state", kind, 0))))
		cfg := rv64I31Config()
		features := cfg.frontendFeatures()
		features.ExceptionHandling, features.ExceptionReferences = true, true
		c, err := compileWithFrontendFeatures(cfg, data, features)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		blob, err := c.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		var loaded Compiled
		if err := loaded.UnmarshalBinary(blob); err != nil {
			t.Fatal(err)
		}
		defer loaded.Close()
		for _, compiled := range []*Compiled{c, &loaded} {
			in, err := instantiateCore(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if !compiled.needsOwnedExceptionReferences() || in.gc == nil {
				t.Fatal("exception state lost collector ownership")
			}
			if err := in.Close(); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestRV64ExceptionCollectorFieldSurvivesCollection(t *testing.T) {
	capture := []byte{0x02, 0x69, 0x1f, 0x40, 1, 3, 0, 0x20, 0, 0x08, 0, 0x0b, 0x00, 0x0b, 0x0b}
	run := []byte{1, 1, 0x63, 0, 0x41, 42, 0x10, 0, 0xfb, 0, 0, 0x21, 0,
		0x41, 0xe3, 0, 0x10, 0, 0x1a, 0x02, 0x7f, 0x1f, 0x7f, 1, 0, 0, 0,
		0x20, 0, 0xfb, 2, 0, 0, 0x0a, 0x41, 0, 0x0b, 0x0b, 0x0b}
	data := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x69, 0}, wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil), []byte{0x60, 1, 0x7f, 1, 0x63, 0x69}, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{2}, []byte{3})), wasmtest.Section(13, wasmtest.Vec([]byte{0, 1})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(capture), append(wasmtest.ULEB(uint32(len(run))), run...))))
	cfg := rv64I31Config().WithCoreFeatures(platformCoreFeatures() | CoreFeatureGC | CoreFeatureTypedFunctionReferences)
	features := cfg.frontendFeatures()
	features.ExceptionHandling, features.ExceptionReferences = true, true
	c, err := compileWithFrontendFeatures(cfg, data, features)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	blob, err := c.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	var loaded Compiled
	if err := loaded.UnmarshalBinary(blob); err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	for _, compiled := range []*Compiled{c, &loaded} {
		in, err := instantiateCore(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 32; i++ {
			if got, err := in.Invoke("run"); err != nil || len(got) != 1 || got[0] != 42 {
				t.Fatalf("round %d: result=%v error=%v", i, got, err)
			}
		}
		if err := in.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func stagedExceptionHandlingModule() []byte {
	tagSig := wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, nil)
	catchSig := wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32})
	pairSig := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32, wasm.I32})
	tag := []byte{0x00, 0x00} // attribute 0, type index 0
	thrower := []byte{0x20, 0x00, 0x20, 0x01, 0x08, 0x00, 0x0b}
	catcher := []byte{
		0x02, 0x02, // block (type 2): results i32, i32
		0x1f, 0x40, 0x01, 0x00, 0x00, 0x00, // try_table void, catch tag 0 -> label 0
		0x20, 0x02, 0x04, 0x40, 0x00, 0x0b, // if control != 0: unreachable
		0x20, 0x00, 0x20, 0x01, 0x10, 0x00, 0x00, // nested call thrower; unreachable if it returns
		0x0b, 0x00, // end try_table; normal continuation is unreachable
		0x0b,                   // end payload block
		0x21, 0x01, 0x21, 0x00, // preserve payload order in params 0/1
		0x20, 0x00, 0x41, 0x0a, 0x6c, 0x20, 0x01, 0x6a,
		0x0b,
	}
	uncaught := []byte{0x20, 0x00, 0x20, 0x01, 0x10, 0x00, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(tagSig, catchSig, pairSig)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(1))),
		wasmtest.Section(13, wasmtest.Vec(tag)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("catch", 0, 1),
			wasmtest.ExportEntry("uncaught", 0, 2),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(thrower), wasmtest.Code(catcher), wasmtest.Code(uncaught))),
	)
}

func stagedExceptionHandlingGeneralModule() []byte {
	types := [][]byte{
		wasmtest.FuncType(nil, nil),
		wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil),
		wasmtest.FuncType([]wasm.ValType{wasm.I64}, nil),
		wasmtest.FuncType([]wasm.ValType{wasm.F32}, nil),
		wasmtest.FuncType([]wasm.ValType{wasm.F64}, nil),
		wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I64}, nil),
		wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}),
		wasmtest.FuncType([]wasm.ValType{wasm.F32}, []wasm.ValType{wasm.F32}),
		wasmtest.FuncType([]wasm.ValType{wasm.F64}, []wasm.ValType{wasm.F64}),
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32, wasm.I64}),
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	}
	tags := make([][]byte, 6)
	for i := range tags {
		tags[i] = []byte{0x00, byte(i)}
	}
	throwers := [][]byte{
		{0x08, 0x00, 0x0b},
		{0x20, 0x00, 0x08, 0x01, 0x0b},
		{0x20, 0x00, 0x08, 0x02, 0x0b},
		{0x20, 0x00, 0x08, 0x03, 0x0b},
		{0x20, 0x00, 0x08, 0x04, 0x0b},
		{0x20, 0x00, 0x20, 0x01, 0x08, 0x05, 0x0b},
	}
	echo := func(tag, thrower byte, blockType byte, zero []byte) []byte {
		body := []byte{0x02, blockType, 0x1f, blockType, 0x01, 0x00, tag, 0x00, 0x20, 0x00, 0x10, thrower}
		body = append(body, zero...)
		return append(body, 0x0b, 0x0b, 0x0b)
	}
	pair := []byte{
		0x02, 0x0a, // block type 10: (result i32 i64)
		0x1f, 0x0a, 0x01, 0x00, 0x05, 0x00,
		0x41, 0x0b, 0x42, 0x16, 0x10, 0x05,
		0x41, 0x00, 0x42, 0x00,
		0x0b, 0x0b, 0x0b,
	}
	ordered := []byte{
		0x02, 0x7f, // outer i32
		0x02, 0x40, // catch-all target
		0x1f, 0x40, 0x03,
		0x00, 0x01, 0x01, // catch tag 1 -> outer
		0x00, 0x00, 0x00, // catch tag 0 -> catch-all target
		0x02, 0x00, // catch_all -> catch-all target
		0x20, 0x00, 0x45, 0x04, 0x40,
		0x41, 0x37, 0x10, 0x01, // selector 0: tag 1 payload 55
		0x05,
		0x20, 0x00, 0x41, 0x01, 0x46, 0x04, 0x40,
		0x10, 0x00, // selector 1: tag 0
		0x05,
		0x42, 0x09, 0x10, 0x02, // other: tag 2, caught by catch_all
		0x0b, 0x0b,
		0x41, 0x07, 0x0c, 0x02, // normal result (unreached by these cases)
		0x0b, // try_table
		0x0b, // catch-all target
		0x41, 0x09,
		0x0b, 0x0b,
	}
	nested := []byte{
		0x02, 0x40,
		0x1f, 0x40, 0x01, 0x00, 0x00, 0x00, // outer catches tag 0
		0x02, 0x7f,
		0x1f, 0x7f, 0x01, 0x00, 0x01, 0x00, // inner catches tag 1 payload
		0x10, 0x00, 0x41, 0x00, // throw tag 0; fallback satisfies normal result
		0x0b, 0x0b, 0x1a, 0x0b,
		0x41, 0x01, 0x0f,
		0x0b,
		0x41, 0x02, 0x0b,
	}
	sequential := []byte{
		0x02, 0x40, 0x1f, 0x40, 0x01, 0x00, 0x00, 0x00, 0x10, 0x00, 0x0b, 0x0b,
		0x02, 0x40, 0x1f, 0x40, 0x01, 0x00, 0x00, 0x00, 0x10, 0x00, 0x0b, 0x0b,
		0x41, 0x07, 0x0b,
	}
	bodies := append(throwers,
		echo(1, 1, 0x7f, []byte{0x41, 0x00}),
		echo(2, 2, 0x7e, []byte{0x42, 0x00}),
		echo(3, 3, 0x7d, []byte{0x43, 0, 0, 0, 0}),
		echo(4, 4, 0x7c, []byte{0x44, 0, 0, 0, 0, 0, 0, 0, 0}),
		pair, ordered, nested, sequential,
	)
	funcTypes := [][]byte{{0}, {1}, {2}, {3}, {4}, {5}, {6}, {7}, {8}, {9}, {10}, {6}, {11}, {11}}
	codes := make([][]byte, len(bodies))
	for i := range bodies {
		codes[i] = wasmtest.Code(bodies[i])
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(funcTypes...)),
		wasmtest.Section(13, wasmtest.Vec(tags...)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("i32", 0, 6),
			wasmtest.ExportEntry("i64", 0, 7),
			wasmtest.ExportEntry("f32", 0, 8),
			wasmtest.ExportEntry("f64", 0, 9),
			wasmtest.ExportEntry("pair", 0, 10),
			wasmtest.ExportEntry("ordered", 0, 11),
			wasmtest.ExportEntry("nested", 0, 12),
			wasmtest.ExportEntry("sequential", 0, 13),
		)),
		wasmtest.Section(10, wasmtest.Vec(codes...)),
	)
}

func stagedExceptionHandlingTagExportModule() []byte {
	tagSig := wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, nil)
	fnSig := wasmtest.FuncType(nil, nil)
	body := []byte{0x41, 0x01, 0x41, 0x02, 0x08, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(tagSig, fnSig)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(13, wasmtest.Vec([]byte{0x00, 0x00})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("tag", 4, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func stagedExceptionReferenceModule() []byte {
	voidSig := wasmtest.FuncType(nil, nil)
	exnSig := []byte{0x60, 0x00, 0x01, 0x63, 0x69} // () -> (ref null exn)
	thrower := []byte{0x08, 0x00, 0x0b}
	rethrow := []byte{
		0x02, 0x01, // block type 1: (result exnref)
		0x1f, 0x40, 0x01, 0x01, 0x00, 0x00, // try_table void, catch_ref tag 0 -> label 0
		0x10, 0x00, // nested local thrower
		0x0b, // end try_table
		0x00, // normal fallthrough is unreachable
		0x0b, // end block with rooted exnref
		0x0a, // throw_ref
		0x0b,
	}
	nullThrow := []byte{0xd0, 0x69, 0x0a, 0x0b} // ref.null exn; throw_ref
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(voidSig, exnSig)),
		wasmtest.Section(3, wasmtest.Vec([]byte{0x00}, []byte{0x00}, []byte{0x00})),
		wasmtest.Section(13, wasmtest.Vec([]byte{0x00, 0x00})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("rethrow", 0, 1),
			wasmtest.ExportEntry("null", 0, 2),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(thrower), wasmtest.Code(rethrow), wasmtest.Code(nullThrow))),
	)
}

func stagedInternalExceptionSignatureModule(export bool) []byte {
	exnSig := []byte{0x60, 0x00, 0x01, 0x63, 0x69} // () -> (ref null exn)
	sections := [][]byte{
		wasmtest.Section(1, wasmtest.Vec(exnSig, wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0x00})),
		wasmtest.Section(13, wasmtest.Vec([]byte{0x00, 0x01})),
	}
	if export {
		sections = append(sections, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("exn", byte(wasm.ExternFunc), 0))))
	}
	sections = append(sections, wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x00, 0x0b}))))
	return wasmtest.Module(sections...)
}

func stagedExceptionHandlingStartModule() []byte {
	tagSig := wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, nil)
	startSig := wasmtest.FuncType(nil, nil)
	tag := []byte{0x00, 0x00}
	start := []byte{0x41, 0x07, 0x41, 0x08, 0x08, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(tagSig, startSig)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(13, wasmtest.Vec(tag)),
		wasmtest.Section(8, wasmtest.ULEB(0)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(start))),
	)
}

func TestStagedExceptionHandlingLocalScalarExecution(t *testing.T) {
	data := stagedExceptionHandlingModule()
	if _, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2), data); err == nil || !strings.Contains(err.Error(), "exception-handling") {
		t.Fatalf("Core 2 compile = %v, want closed exception-handling gate", err)
	}
	c, err := compileStagedExceptionHandlingFeaturesForTest(data, false)
	if err != nil {
		t.Fatalf("default Core 3 compile: %v", err)
	}
	defer c.Close()
	if !c.requiredFeatures.IsEnabled(CoreFeatureExceptionHandling) {
		t.Fatal("compiled module lost exception-handling required feature")
	}
	meta := (&Module{c: c}).Metadata()
	if len(meta.Tags) != 1 || meta.Tags[0].Index != 0 || meta.Tags[0].TypeIndex != 0 || len(meta.Tags[0].Params) != 2 || meta.Tags[0].Params[0] != ValI32 || meta.Tags[0].Params[1] != ValI32 {
		t.Fatalf("tag metadata = %#v", meta.Tags)
	}
	blob, err := c.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary staged EH: %v", err)
	}
	var loaded Compiled
	if err := loaded.UnmarshalBinary(blob); err != nil {
		t.Fatalf("UnmarshalBinary staged EH: %v", err)
	}
	if got := (&Module{c: &loaded}).Metadata().Tags; !reflect.DeepEqual(got, meta.Tags) {
		t.Fatalf("reloaded EH tag metadata = %#v, want %#v", got, meta.Tags)
	}
	loadedInstance, err := instantiateCore(&loaded, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate reloaded EH: %v", err)
	}
	for round := 0; round < 32; round++ {
		got, err := loadedInstance.Invoke("catch", I32(4), I32(2), I32(0))
		if err != nil || len(got) != 1 || got[0] != 42 {
			t.Fatalf("reloaded catch=%v,%v", got, err)
		}
	}
	_ = loadedInstance.Close()
	_ = loaded.Close()

	in, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate staged exception handling: %v", err)
	}
	defer in.Close()
	if got, err := in.Invoke("uncaught", I32(1), I32(2), I32(0)); err == nil || !strings.Contains(err.Error(), "unhandled WebAssembly exception") {
		t.Fatalf("initial uncaught exception result=%v err=%v", got, err)
	}
	got, err := in.Invoke("catch", I32(4), I32(2), I32(0))
	if err != nil || len(got) != 1 || uint32(got[0]) != 42 {
		t.Fatalf("catch result=%v err=%v, want 42", got, err)
	}
	if _, err := in.Invoke("catch", I32(4), I32(2), I32(1)); err == nil || !strings.Contains(err.Error(), "unreachable") {
		t.Fatalf("trap inside active try_table = %v", err)
	}
	got, err = in.Invoke("catch", I32(9), I32(7), I32(0))
	if err != nil || len(got) != 1 || uint32(got[0]) != 97 {
		t.Fatalf("post-trap catch result=%v err=%v, want 97", got, err)
	}
	if got, err := in.Invoke("uncaught", I32(1), I32(2), I32(0)); err == nil || !strings.Contains(err.Error(), "unhandled WebAssembly exception") {
		t.Fatalf("uncaught exception result=%v err=%v", got, err)
	}
	allocs := testing.AllocsPerRun(1000, func() {
		got, err := in.Invoke("catch", I32(4), I32(2), I32(0))
		if err != nil || len(got) != 1 || uint32(got[0]) != 42 {
			panic("staged EH repeated catch failed")
		}
	})
	if allocs != 0 {
		t.Fatalf("steady-state caught exception allocations = %v, want 0", allocs)
	}
	for i := 0; i < 10_000; i++ {
		if _, err := in.Invoke("catch", I32(4), I32(2), I32(0)); err != nil {
			t.Fatalf("repeated caught exception %d: %v", i, err)
		}
	}
}

func TestStagedExceptionHandlingGeneralLocalScalarExecution(t *testing.T) {
	c := compileStagedExceptionHandling(t, stagedExceptionHandlingGeneralModule())
	defer c.Close()
	meta := (&Module{c: c}).Metadata()
	if len(meta.Tags) != 6 {
		t.Fatalf("tag metadata count = %d, want 6", len(meta.Tags))
	}
	in, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate generalized staged EH: %v", err)
	}
	defer in.Close()
	if got, err := in.Invoke("pair"); err != nil || len(got) != 2 || uint32(got[0]) != 11 || got[1] != 22 {
		t.Fatalf("pair result=%v err=%v, want [11 22]", got, err)
	}
	for _, tc := range []struct {
		name string
		arg  uint64
		want uint64
	}{
		{name: "i32", arg: I32(-17), want: I32(-17)},
		{name: "i64", arg: I64(-0x123456789), want: I64(-0x123456789)},
		{name: "f32", arg: F32(10.5), want: F32(10.5)},
		{name: "f64", arg: F64(-19.25), want: F64(-19.25)},
	} {
		got, err := in.Invoke(tc.name, tc.arg)
		if err != nil || len(got) != 1 || got[0] != tc.want {
			t.Fatalf("%s result=%v err=%v, want %#x", tc.name, got, err, tc.want)
		}
	}
	for selector, want := range []uint32{55, 9, 9} {
		got, err := in.Invoke("ordered", I32(int32(selector)))
		if err != nil || len(got) != 1 || uint32(got[0]) != want {
			t.Fatalf("ordered(%d) result=%v err=%v, want %d", selector, got, err, want)
		}
	}
	if got, err := in.Invoke("nested"); err != nil || len(got) != 1 || uint32(got[0]) != 2 {
		t.Fatalf("nested result=%v err=%v, want 2", got, err)
	}
	if got, err := in.Invoke("sequential"); err != nil || len(got) != 1 || uint32(got[0]) != 7 {
		t.Fatalf("sequential result=%v err=%v, want 7", got, err)
	}
	allocs := testing.AllocsPerRun(1000, func() {
		got, err := in.Invoke("ordered", I32(0))
		if err != nil || len(got) != 1 || uint32(got[0]) != 55 {
			panic("general staged EH repeated catch failed")
		}
	})
	if allocs != 0 {
		t.Fatalf("general caught exception allocations = %v, want 0", allocs)
	}
}

func TestStagedExceptionHandlingRootedReferenceNestedCallAndNullTrap(t *testing.T) {
	data := stagedExceptionReferenceModule()
	if _, err := compileStagedExceptionHandlingFeaturesForTest(data, false); err == nil {
		t.Fatal("exception references compiled with the exception-reference feature disabled")
	}
	c := compileStagedExceptionHandlingFeatures(t, data, true)
	defer c.Close()
	in, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate rooted exception references: %v", err)
	}
	defer in.Close()
	if got, err := in.Invoke("rethrow"); err == nil || !strings.Contains(err.Error(), "unhandled WebAssembly exception") {
		t.Fatalf("nested catch_ref/throw_ref result=%v err=%v", got, err)
	}
	if got, err := in.Invoke("null"); err == nil || !strings.Contains(err.Error(), "null reference") {
		t.Fatalf("null throw_ref result=%v err=%v", got, err)
	}
	for i := 0; i < 10_000; i++ {
		if _, err := in.Invoke("rethrow"); err == nil || !strings.Contains(err.Error(), "unhandled WebAssembly exception") {
			t.Fatalf("repeated nested rethrow %d: %v", i, err)
		}
	}
}

func TestStagedExceptionReferenceInternalProductCategory(t *testing.T) {
	c := compileStagedExceptionHandlingFeatures(t, stagedInternalExceptionSignatureModule(false), true)
	defer c.Close()
	meta := (&Module{c: c}).Metadata()
	if len(meta.Functions) != 1 || !reflect.DeepEqual(meta.Functions[0].Results, []ValType{ValExnRef}) {
		t.Fatalf("internal exception signature metadata = %#v", meta.Functions)
	}
	blob, err := c.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal internal exception signature: %v", err)
	}
	var loaded Compiled
	if err := loaded.UnmarshalBinary(blob); err != nil {
		t.Fatalf("unmarshal internal exception signature: %v", err)
	}
	defer loaded.Close()
	if got := (&Module{c: &loaded}).Metadata().Functions[0].Results; !reflect.DeepEqual(got, []ValType{ValExnRef}) {
		t.Fatalf("reloaded exception signature = %v", got)
	}
	exported, err := compileStagedExceptionHandlingFeaturesForTest(stagedInternalExceptionSignatureModule(true), true)
	if err != nil {
		t.Fatalf("exported exception signature compile = %v", err)
	}
	defer exported.Close()
}

func TestStagedExceptionHandlingStartFailsClosed(t *testing.T) {
	c := compileStagedExceptionHandling(t, stagedExceptionHandlingStartModule())
	defer c.Close()
	if in, err := instantiateCore(c, InstantiateOptions{}); err == nil {
		_ = in.Close()
		t.Fatal("uncaught start exception instantiated")
	} else if !strings.Contains(err.Error(), "unhandled WebAssembly exception") {
		t.Fatalf("start error = %v", err)
	}
}

func TestStagedExceptionHandlingProductAndPlatformGates(t *testing.T) {
	cfg := NewRuntimeConfig().WithBoundsChecks(BoundsChecksSignalsBased)
	features := cfg.frontendFeatures()
	features.ExceptionHandling = true
	guarded, err := compileWithFrontendFeatures(cfg, stagedExceptionHandlingModule(), features)
	if err != nil {
		t.Fatalf("guard-mode staged EH: %v", err)
	}
	_ = guarded.Close()

	exported, err := compileWithFrontendFeatures(NewRuntimeConfig(), stagedExceptionHandlingTagExportModule(), features)
	if err != nil {
		t.Fatalf("tag-export staged EH compile: %v", err)
	}
	_ = exported.Close()
}

func BenchmarkStagedExceptionHandlingCatch(b *testing.B) {
	c := compileStagedExceptionHandling(b, stagedExceptionHandlingModule())
	defer c.Close()
	in, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, err := in.Invoke("catch", I32(4), I32(2), I32(0))
		if err != nil || len(got) != 1 || uint32(got[0]) != 42 {
			b.Fatalf("result=%v err=%v", got, err)
		}
	}
}

func BenchmarkStagedExceptionHandlingCompile(b *testing.B) {
	data := stagedExceptionHandlingGeneralModule()
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		compiled := compileStagedExceptionHandling(b, data)
		if err := compiled.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGCFrameRootPlanSparseExceptionRoots(b *testing.B) {
	indexedFuncParam := []byte{0x60, 0x01, 0x64, 0x00, 0x00} // (func (param (ref 0)))
	body := []byte{0x02, 0x40, 0x1f, 0x40, 0x01, byte(wasm.CatchRef), 0x00, 0x00, 0x01, 0x0b, 0x0b, 0x0b}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil), indexedFuncParam)),
		wasmtest.Section(3, wasmtest.Vec([]byte{0x00}, []byte{0x00})),
		wasmtest.Section(13, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body), wasmtest.Code([]byte{0x0b}))),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		b.Fatal(err)
	}
	const functions = 1024
	funcType, code := m.FuncTypes[len(m.FuncTypes)-1], m.Code[len(m.Code)-1]
	for len(m.Code) < functions {
		m.FuncTypes = append(m.FuncTypes, funcType)
		m.Code = append(m.Code, code)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		var diagnostic string
		gcFrameRootPlanSink = newGCFrameRootPlan(m, true, true, &diagnostic, nil)
		if gcFrameRootPlanSink == nil || diagnostic != "" {
			b.Fatalf("exact root plan = %+v, diagnostic = %q", gcFrameRootPlanSink, diagnostic)
		}
	}
}

func TestRV64ExceptionVectorTransport(t *testing.T) {
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil), wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.V128}, []wasm.ValType{wasm.V128}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(13, wasmtest.Vec([]byte{0, 0})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x02, 0x7f, 0x1f, 0x40, 1, 0, 0, 0, 0x20, 0, 0x08, 0, 0x0b, 0x00, 0x0b, 0x1a, 0x20, 1, 0x0b}))),
	)
	c := compileStagedExceptionHandling(t, data)
	defer c.Close()
	in, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	for i := 0; i < 32; i++ {
		got, err := in.Invoke("run", I32(42), uint64(0x123456789abcdef), uint64(0xfedcba987654321))
		if err != nil || len(got) != 2 || got[0] != 0x123456789abcdef || got[1] != 0xfedcba987654321 {
			t.Fatalf("vector EH = %x, %v", got, err)
		}
	}
}

func TestRV64ExceptionCollectorPayload(t *testing.T) {
	body := []byte{0x02, byte(wasm.HeapExn), 0x1f, 0x40, 1, byte(wasm.CatchAllRef), 0,
		0x41, 42, 0xfb, 0, 0, 0x08, 0, 0x0b, 0x00, 0x0b, 0x21, 0,
		0x41, 7, 0xfb, 0, 0, 0x1a,
		0x02, 3, 0x1f, 0x40, 1, byte(wasm.CatchTag), 0, 0, 0x20, 0, 0x0a, 0x0b, 0x00, 0x0b,
		0xfb, 2, 0, 0, 0x0b}
	code := append([]byte{1, 1, byte(wasm.HeapExn)}, body...)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 0}, []byte{0x60, 1, 0x63, 0, 0}, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), []byte{0x60, 0, 1, 0x63, 0})),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))), wasmtest.Section(13, wasmtest.Vec([]byte{0, 1})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(code))), code...))))
	cfg := rv64I31Config().WithCoreFeatures(platformCoreFeatures() | CoreFeatureGC | CoreFeatureTypedFunctionReferences)
	features := cfg.frontendFeatures()
	features.ExceptionHandling = true
	features.ExceptionReferences = true
	c, err := compileWithFrontendFeatures(cfg, data, features)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	blob, err := c.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	var loaded Compiled
	if err := loaded.UnmarshalBinary(blob); err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	in, err := instantiateCore(&loaded, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	for i := 0; i < 32; i++ {
		got, err := in.Invoke("run")
		if err != nil || len(got) != 1 || got[0] != 42 {
			t.Fatalf("collector EH=%v,%v", got, err)
		}
	}
}

func TestRV64ExceptionVectorTagPayload(t *testing.T) {
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.V128, wasm.I32}, nil), wasmtest.FuncType([]wasm.ValType{wasm.V128, wasm.I32}, []wasm.ValType{wasm.V128}), wasmtest.FuncType(nil, []wasm.ValType{wasm.V128, wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))), wasmtest.Section(13, wasmtest.Vec([]byte{0, 0})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x02, 2, 0x1f, 0x40, 1, byte(wasm.CatchTag), 0, 0, 0x20, 0, 0x20, 1, 0x08, 0, 0x0b, 0x00, 0x0b, 0x1a, 0x0b}))))
	c := compileStagedExceptionHandling(t, data)
	defer c.Close()
	blob, err := c.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	var loaded Compiled
	if err := loaded.UnmarshalBinary(blob); err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	in, err := instantiateCore(&loaded, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	for i := 0; i < 32; i++ {
		got, err := in.Invoke("run", 0x123456789abcdef, 0xfedcba987654321, I32(42))
		if err != nil || len(got) != 2 || got[0] != 0x123456789abcdef || got[1] != 0xfedcba987654321 {
			t.Fatalf("vector tag payload=%x,%v", got, err)
		}
	}
}

func TestRV64ExceptionTaglessNullReference(t *testing.T) {
	data := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xd0, byte(wasm.HeapExn), 0x0a, 0x0b}))))
	c := compileStagedExceptionHandlingFeatures(t, data, true)
	defer c.Close()
	blob, err := c.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	var loaded Compiled
	if err := loaded.UnmarshalBinary(blob); err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	in, err := instantiateCore(&loaded, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	for i := 0; i < 32; i++ {
		if _, err := in.Invoke("run"); err == nil || !strings.Contains(err.Error(), "null reference") {
			t.Fatalf("tagless null throw_ref=%v", err)
		}
	}
}

func TestRV64ExceptionHostReentryRestoresHandler(t *testing.T) {
	rt := NewRuntime(WithRuntimeConfig(rv64I31Config().WithCoreFeatures(platformCoreFeatures() | CoreFeatureExceptionHandling)))
	defer rt.Close()
	providerCode := compileStagedExceptionHandling(t, stagedCrossInstanceEHProviderModule())
	defer providerCode.Close()
	providerModule, err := rt.Module(providerCode)
	if err != nil {
		t.Fatal(err)
	}
	defer providerModule.Close()
	provider, err := rt.Instantiate(context.Background(), providerModule)
	if err != nil {
		t.Fatal(err)
	}
	defer provider.Close()
	calls := 0
	host, err := rt.NewHostFuncRef(slotHostFunc(func(module HostModule, params, results []uint64) {
		calls++
		if _, err := provider.InvokeFromHost(context.Background(), module, "throw"); err == nil || !strings.Contains(err.Error(), "unhandled WebAssembly exception") {
			panic("nested throw did not remain uncaught")
		}
	}), FuncSig{})
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	data := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(stagedFuncImportEntry("env", "host", 2))), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))), wasmtest.Section(13, wasmtest.Vec([]byte{0, 0})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x02, 0x7f, 0x1f, 0x40, 1, byte(wasm.CatchTag), 0, 0, 0x10, 0, 0x41, 42, 0x08, 0, 0x0b, 0x00, 0x0b, 0x0b}))))
	c := compileStagedExceptionHandling(t, data)
	defer c.Close()
	module, err := rt.Module(c)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	in, err := rt.Instantiate(context.Background(), module, WithImports(testImports("env.host", host)))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	for i := 0; i < 32; i++ {
		got, err := in.Invoke("run")
		if err != nil || len(got) != 1 || got[0] != 42 {
			t.Fatalf("host reentry EH=%v,%v", got, err)
		}
	}
	if calls != 32 {
		t.Fatalf("host calls=%d", calls)
	}
}

func TestRV64ExceptionPlainExternTag(t *testing.T) {
	for _, vector := range []bool{false, true} {
		body := []byte{0x02, 0x6f, 0x1f, 0x40, 1, byte(wasm.CatchTag), 0, 0, 0x20, 0, 0x08, 0, 0x0b, 0, 0x0b}
		if vector {
			body = append(body, 0xfd, 12)
			body = append(body, make([]byte, 16)...)
			body = append(body, 0x1a)
		}
		body = append(body, 0x0b)
		data := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.ExternRef}, nil), wasmtest.FuncType([]wasm.ValType{wasm.ExternRef}, []wasm.ValType{wasm.ExternRef}))), wasmtest.Section(3, wasmtest.Vec([]byte{1})), wasmtest.Section(13, wasmtest.Vec([]byte{0, 0})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
		cfg := rv64I31Config()
		features := cfg.frontendFeatures()
		features.ExceptionHandling, features.ExceptionReferences = true, true
		source, err := compileWithFrontendFeatures(cfg, data, features)
		if err != nil {
			t.Fatal(err)
		}
		defer source.Close()
		blob, err := source.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		artifact, err := LoadTrustedArtifact(blob)
		if err != nil {
			t.Fatal(err)
		}
		defer artifact.Close()
		for _, c := range []*Compiled{source, artifact} {
			if c.needsOwnedExceptionReferences() {
				t.Fatal("plain extern catch selected owned exception storage")
			}
			rt := NewRuntime(WithRuntimeConfig(cfg))
			defer rt.Close()
			mod, err := rt.Module(c)
			if err != nil {
				t.Fatal(err)
			}
			in, err := rt.Instantiate(context.Background(), mod)
			mod.Close()
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			if in.gc != nil {
				t.Fatal("plain extern catch allocated a collector")
			}
			ref, err := rt.NewExternRef("plain extern exception")
			if err != nil {
				t.Fatal(err)
			}
			got, err := in.InvokeValues(context.Background(), "run", ValueExternRef(ref))
			if err != nil || len(got) != 1 || got[0].ExternRef() != ref {
				t.Fatalf("plain extern catch %v,%v", got, err)
			}
			if !rt.ReleaseExternRef(ref) {
				t.Fatal("extern reference release")
			}
		}
	}
}

func TestRV64ExceptionImplicitExternStoreBinding(t *testing.T) {
	payloadImport := append(wasmtest.Name("env"), wasmtest.Name("payload")...)
	providerBytes := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.ExternRef}, nil), wasmtest.FuncType(nil, []wasm.ValType{wasm.ExternRef}), wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(append(payloadImport, 0, 1))),
		wasmtest.Section(3, wasmtest.Vec([]byte{2})),
		wasmtest.Section(13, wasmtest.Vec([]byte{0, 0})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("throw", 0, 1), wasmtest.ExportEntry("tag", 4, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0, 0x08, 0, 0x0b}))))
	throwImport := append(wasmtest.Name("env"), wasmtest.Name("throw")...)
	tagImport := append(wasmtest.Name("env"), wasmtest.Name("tag")...)
	consumerBytes := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.ExternRef}, nil), wasmtest.FuncType(nil, nil), wasmtest.FuncType(nil, []wasm.ValType{wasm.ExternRef}))),
		wasmtest.Section(2, wasmtest.Vec(append(throwImport, 0, 1), append(tagImport, 4, 0, 0))),
		wasmtest.Section(3, wasmtest.Vec([]byte{2})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x02, 0x6f, 0x1f, 0x40, 1, byte(wasm.CatchTag), 0, 0, 0x10, 0, 0x0b, 0, 0x0b, 0x0b}))))
	cfg := rv64I31Config()
	features := cfg.frontendFeatures()
	features.ExceptionHandling, features.ExceptionReferences = true, true
	providerCode, err := compileWithFrontendFeatures(cfg, providerBytes, features)
	if err != nil {
		t.Fatal(err)
	}
	defer providerCode.Close()
	consumerCode, err := compileWithFrontendFeatures(cfg, consumerBytes, features)
	if err != nil {
		t.Fatal(err)
	}
	defer consumerCode.Close()
	for _, artifact := range []bool{false, true} {
		c := consumerCode
		if artifact {
			blob, err := c.MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}
			c, err = LoadTrustedArtifact(blob)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
		}
		rt := NewRuntime(WithRuntimeConfig(cfg))
		defer rt.Close()
		ref, err := rt.NewExternRef("implicit extern payload")
		if err != nil {
			t.Fatal(err)
		}
		providerModule, err := rt.Module(providerCode)
		if err != nil {
			t.Fatal(err)
		}
		provider, err := rt.Instantiate(context.Background(), providerModule, WithImports(testImports("env.payload", slotHostFunc(func(_ HostModule, _, result []uint64) { result[0] = ref.token }))))
		providerModule.Close()
		if err != nil {
			t.Fatal(err)
		}
		defer provider.Close()
		throw, err := provider.ExportedFunc("throw")
		if err != nil {
			t.Fatal(err)
		}
		tag, err := provider.ExportedTag("tag")
		if err != nil {
			t.Fatal(err)
		}
		imports := NewImports()
		imports.Function("env", "throw", throw)
		imports.Tag("env", "tag", tag)
		foreign := NewRuntime(WithRuntimeConfig(cfg))
		defer foreign.Close()
		foreignModule, err := foreign.Module(c)
		if err != nil {
			t.Fatal(err)
		}
		rejected, err := foreign.Instantiate(context.Background(), foreignModule, WithImports(imports))
		foreignModule.Close()
		if rejected != nil || err == nil || !strings.Contains(err.Error(), "same reference store") {
			t.Fatalf("cross-store implicit extern import accepted=%v error=%v", rejected != nil, err)
		}
		consumerModule, err := rt.Module(c)
		if err != nil {
			t.Fatal(err)
		}
		consumer, err := rt.Instantiate(context.Background(), consumerModule, WithImports(imports))
		consumerModule.Close()
		if err != nil {
			t.Fatal(err)
		}
		provider.Close()
		for range 16 {
			got, err := consumer.InvokeValues(context.Background(), "run")
			if err != nil || len(got) != 1 || got[0].ExternRef() != ref {
				t.Fatalf("implicit extern payload %v,%v", got, err)
			}
		}
		consumer.Close()
		if !rt.ReleaseExternRef(ref) {
			t.Fatal("extern reference release")
		}
	}
}

func TestRV64ExceptionReferenceControlCarriers(t *testing.T) {
	for _, heap := range []wasm.AbsHeapType{wasm.HeapFunc, wasm.HeapNoFunc, wasm.HeapExtern, wasm.HeapNoExtern, wasm.HeapExn, wasm.HeapNoExn} {
		for _, form := range []string{"bare", "explicit", "select"} {
			for _, vector := range []bool{false, true} {
				t.Run(fmt.Sprintf("heap=%s/%s/vector=%v", heap, form, vector), func(t *testing.T) {
					body := []byte{0x1f, 0x40, 0, 0x0b}
					if form == "select" {
						body = append(body, 0xd0, byte(heap), 0xd0, byte(heap), 0x41, 1, 0x1c, 1, byte(heap))
					} else {
						body = append(body, 0x02)
						if form == "explicit" {
							body = append(body, 0x63)
						}
						body = append(body, byte(heap), 0xd0, byte(heap), 0x0b)
					}
					body = append(body, 0xd1)
					if vector {
						body = append(body, 0xfd, 12)
						body = append(body, make([]byte, 16)...)
						body = append(body, 0x1a)
					}
					body = append(body, 0x0b)
					data := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))), wasmtest.Section(3, wasmtest.Vec([]byte{1})), wasmtest.Section(13, wasmtest.Vec([]byte{0, 0})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
					cfg := rv64I31Config().WithCoreFeatures(platformCoreFeatures() | CoreFeatureGC | CoreFeatureTypedFunctionReferences)
					features := cfg.frontendFeatures()
					features.ExceptionHandling, features.ExceptionReferences = true, true
					source, err := compileWithFrontendFeatures(cfg, data, features)
					if err != nil {
						t.Fatal(err)
					}
					defer source.Close()
					blob, err := source.MarshalBinary()
					if err != nil {
						t.Fatal(err)
					}
					artifact, err := LoadTrustedArtifact(blob)
					if err != nil {
						t.Fatal(err)
					}
					defer artifact.Close()
					for _, c := range []*Compiled{source, artifact} {
						in, err := instantiateCore(c, InstantiateOptions{})
						if err != nil {
							t.Fatal(err)
						}
						got, err := in.Invoke("run")
						in.Close()
						if err != nil || len(got) != 1 || got[0] != 1 {
							t.Fatalf("reference control %v,%v", got, err)
						}
					}
				})
			}
		}
	}
}
