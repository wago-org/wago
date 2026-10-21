//go:build linux && (amd64 || arm64) && !tinygo

package wago

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/abi"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func cowIntegratedModule() []byte {
	segment := func(offset int32, payload string) []byte {
		d := []byte{0, 0x41}
		d = append(d, wasmtest.SLEB32(offset)...)
		d = append(d, 0x0b)
		d = append(d, wasmtest.ULEB(uint32(len(payload)))...)
		return append(d, payload...)
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{1}, []byte{2})),
		wasmtest.Section(5, wasmtest.Vec([]byte{1, 1, 2})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("grow", 0, 0),
			wasmtest.ExportEntry("store", 0, 1),
			wasmtest.ExportEntry("load", 0, 2),
			wasmtest.ExportEntry("mem", 2, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 1, 0x40, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x3a, 0, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x2d, 0, 0, 0x0b}),
		)),
		wasmtest.Section(11, wasmtest.Vec(
			segment(0, "ABC"), segment(1, "xy"), segment(65534, "QR"),
		)),
	)
}

func assertCOWMapping(t *testing.T, in *Instance) {
	t.Helper()
	maps, err := os.ReadFile("/proc/self/maps")
	if err != nil {
		t.Fatal(err)
	}
	prefix := fmt.Sprintf("%x-", in.jm.LinMemBase()-uintptr(abi.BasedataSize))
	for _, line := range strings.Split(string(maps), "\n") {
		if strings.HasPrefix(line, prefix) && strings.Contains(line, "memfd:wago-cow-image") {
			return
		}
	}
	t.Fatalf("linear memory at %s has no private CoW image mapping", prefix)
}

func processPSSKB(t *testing.T) int {
	t.Helper()
	data, err := os.ReadFile("/proc/self/smaps_rollup")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "Pss:") {
			value, err := strconv.Atoi(strings.Fields(line)[1])
			if err != nil {
				t.Fatal(err)
			}
			return value
		}
	}
	t.Fatal("smaps_rollup had no Pss line")
	return 0
}

// This exercises the actual Wago instance path, including ordered overlapping
// active segments, private writes, growth, close, and fresh instantiation.
func TestCOWImageIntegratedInstances(t *testing.T) {
	t.Setenv("WAGO_EXPERIMENT_COW_IMAGE", "1")
	c, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), cowIntegratedModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	a, err := Instantiate(c)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := Instantiate(c)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if indexes := c.loadCompileIndexes(); indexes == nil || indexes.memoryImage == nil {
		t.Fatal("eligible compiled module did not build a shared image")
	}
	assertCOWMapping(t, a)
	assertCOWMapping(t, b)
	for _, in := range []*Instance{a, b} {
		got := in.Memory().UnsafeBytes()
		if !bytes.Equal(got[:3], []byte("Axy")) || !bytes.Equal(got[65534:65536], []byte("QR")) {
			t.Fatalf("active data initialization = %q / %q", got[:3], got[65534:65536])
		}
	}
	a.Memory().UnsafeBytes()[0] = 'z'
	if b.Memory().UnsafeBytes()[0] != 'A' {
		t.Fatal("private memory write reached sibling")
	}
	if _, err := a.Invoke("store", I32(0), I32('Q')); err != nil {
		t.Fatal(err)
	}
	loaded, err := a.Invoke("load", I32(0))
	if err != nil || len(loaded) != 1 || AsI32(loaded[0]) != 'Q' || b.Memory().UnsafeBytes()[0] != 'A' {
		t.Fatalf("guest CoW store/load = %v, %v; sibling=%q", loaded, err, b.Memory().UnsafeBytes()[0])
	}
	result, err := a.Invoke("grow")
	if err != nil || len(result) != 1 || AsI32(result[0]) != 1 {
		t.Fatalf("grow = %v, %v", result, err)
	}
	if got := a.Memory().UnsafeBytes(); len(got) != 2*65536 || got[65536] != 0 {
		t.Fatal("grown page was not zero-filled")
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	d, err := Instantiate(c)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if got := d.Memory().UnsafeBytes(); got[0] != 'A' || got[65536-1] != 'R' {
		t.Fatal("fresh image inherited an earlier instance write")
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if b.Memory().UnsafeBytes()[0] != 'A' || d.Memory().UnsafeBytes()[0] != 'A' {
		t.Fatal("closing compiled module invalidated live image mappings")
	}
}

func TestCOWImageParallelInstantiation(t *testing.T) {
	t.Setenv("WAGO_EXPERIMENT_COW_IMAGE", "1")
	c, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), cowIntegratedModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	const workers = 12
	var wg sync.WaitGroup
	errors := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			in, err := Instantiate(c)
			if err != nil {
				errors <- err
				return
			}
			if got := in.Memory().UnsafeBytes()[0]; got != 'A' {
				errors <- fmt.Errorf("initial byte = %q", got)
			}
			if err := in.Close(); err != nil {
				errors <- err
			}
		}()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
	if indexes := c.loadCompileIndexes(); indexes == nil || indexes.memoryImage == nil {
		t.Fatal("parallel instantiation did not publish one shared image")
	}
}

func TestCOWImageRealYYJSON(t *testing.T) {
	t.Setenv("WAGO_EXPERIMENT_COW_IMAGE", "1")
	data, err := os.ReadFile(filepath.Join("../..", "corpus/workloads/semantic/yyjson/yyjson.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).Compile(data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	a, err := Instantiate(c)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := Instantiate(c)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	assertCOWMapping(t, a)
	assertCOWMapping(t, b)
	for i := 0; i < c.activeDataCount(); i++ {
		d := c.activeDataAt(i)
		if len(d.Bytes) == 0 {
			continue
		}
		off := int(d.Offset.Base)
		if a.Memory().UnsafeBytes()[off] != d.Bytes[0] || b.Memory().UnsafeBytes()[off] != d.Bytes[0] {
			t.Fatalf("segment %d differs from compiled payload", i)
		}
	}
	probe := int(c.activeDataAt(0).Offset.Base)
	original := b.Memory().UnsafeBytes()[probe]
	a.Memory().UnsafeBytes()[probe] ^= 0xff
	if b.Memory().UnsafeBytes()[probe] != original {
		t.Fatal("real module instance write leaked")
	}
}

// Stub imports allow measuring PHP's real compiled module and data image up
// through instantiation. No PHP entrypoint is called with these stubs.
func cowNoopFunctionImports(tb testing.TB, c *Compiled) *Imports {
	tb.Helper()
	if c.HasStart || c.memoryImportCount() != 0 || c.tableImportCount() != 0 || len(c.GlobalImports) != 0 || c.tagImportCount() != 0 {
		tb.Fatal("module requires non-function imports or a start callback")
	}
	ends, _, _, _, exact := c.importModuleEndSections()
	if !exact || len(c.importFuncSigs) != len(c.Imports) {
		tb.Fatal("function import names/signatures unavailable")
	}
	imports := NewImports()
	for i, key := range c.Imports {
		module, name := splitImportKeyAt(key, importModuleEndAt(ends, i))
		sig := c.importFuncSigs[i]
		imports.HostFunc(module, name, CallerHostCallFunc(func(Caller, HostCall) {})).Params(sig.Params...).Results(sig.Results...)
	}
	return imports
}

func TestCOWImageRealPHPInitialization(t *testing.T) {
	t.Setenv("WAGO_EXPERIMENT_COW_IMAGE", "1")
	data, err := os.ReadFile(filepath.Join("../..", "corpus/workloads/applications/php/php.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit).Compile(data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	imports := cowNoopFunctionImports(t, c)
	a, err := Instantiate(c, imports)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := Instantiate(c, imports)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	assertCOWMapping(t, a)
	assertCOWMapping(t, b)
	for i := 0; i < c.activeDataCount(); i++ {
		d := c.activeDataAt(i)
		if len(d.Bytes) == 0 {
			continue
		}
		off := int(d.Offset.Base)
		if a.Memory().UnsafeBytes()[off] != d.Bytes[0] || b.Memory().UnsafeBytes()[off] != d.Bytes[0] {
			t.Fatalf("PHP segment %d differs from compiled payload", i)
		}
	}
	probe := int(c.activeDataAt(0).Offset.Base)
	original := b.Memory().UnsafeBytes()[probe]
	a.Memory().UnsafeBytes()[probe] ^= 0xff
	if b.Memory().UnsafeBytes()[probe] != original {
		t.Fatal("PHP instance write leaked")
	}
}

func TestCOWImageIntegratedPSS(t *testing.T) {
	module, err := os.ReadFile(filepath.Join("../..", "corpus/workloads/applications/php/php.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit).Compile(module)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	imports := cowNoopFunctionImports(t, c)
	for _, count := range []int{1, 10} {
		for _, mode := range []struct {
			name, flag string
		}{{"baseline", "0"}, {"cow", "1"}} {
			t.Run(fmt.Sprintf("%d/%s", count, mode.name), func(t *testing.T) {
				t.Setenv("WAGO_EXPERIMENT_COW_IMAGE", mode.flag)
				instances := make([]*Instance, count)
				for i := range instances {
					instances[i], err = Instantiate(c, imports)
					if err != nil {
						t.Fatal(err)
					}
					defer instances[i].Close()
				}
				var sink byte
				for _, in := range instances {
					memory := in.Memory().UnsafeBytes()
					for i := 0; i < c.activeDataCount(); i++ {
						d := c.activeDataAt(i)
						if len(d.Bytes) > 0 {
							sink ^= memory[d.Offset.Base]
						}
					}
				}
				t.Logf("instances=%d process_pss_kb=%d sink=%d", len(instances), processPSSKB(t), sink)
			})
		}
	}
}

func TestCOWImageFirstUseCost(t *testing.T) {
	php := cowPHPDataOnlyModule(t)
	realPHP, err := os.ReadFile(filepath.Join("../..", "corpus/workloads/applications/php/php.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	yyjson, err := os.ReadFile(filepath.Join("../..", "corpus/workloads/semantic/yyjson/yyjson.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		name        string
		data        []byte
		stubImports bool
	}{{"yyjson-real", yyjson, false}, {"php-data-only", php, false}, {"php-real-stub-imports", realPHP, true}} {
		for _, mode := range []struct {
			name, flag string
		}{{"baseline", "0"}, {"cow", "1"}} {
			t.Run(item.name+"/"+mode.name, func(t *testing.T) {
				t.Setenv("WAGO_EXPERIMENT_COW_IMAGE", mode.flag)
				started := time.Now()
				c, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit).Compile(item.data)
				compileTime := time.Since(started)
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				var imports *Imports
				if item.stubImports {
					imports = cowNoopFunctionImports(t, c)
				}
				started = time.Now()
				in, err := Instantiate(c, imports)
				firstTime := time.Since(started)
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				t.Logf("source_bytes=%d native_code_bytes=%d compile_ms=%.3f first_instance_ms=%.3f", len(item.data), c.CodeSize(), float64(compileTime.Microseconds())/1000, float64(firstTime.Microseconds())/1000)
			})
		}
	}
}

// The PHP-derived fixture retains its real 63,770 ordered active writes but
// omits unrelated function imports, isolating Wago instance initialization.
func cowPHPDataOnlyModule(tb testing.TB) []byte {
	tb.Helper()
	data, err := os.ReadFile(filepath.Join("../..", "corpus/workloads/applications/php/php.wasm"))
	if err != nil {
		tb.Fatal(err)
	}
	original, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).Compile(data)
	if err != nil {
		tb.Fatal(err)
	}
	defer original.Close()
	segments := make([][]byte, original.activeDataCount())
	for i := range segments {
		d := original.activeDataAt(i)
		if d.MemoryIndex != 0 || d.Offset.HasGlobal || len(d.Offset.Expr) != 0 {
			tb.Fatal("PHP source left constant-offset class")
		}
		seg := []byte{0, 0x41}
		seg = append(seg, wasmtest.SLEB32(int32(d.Offset.Base))...)
		seg = append(seg, 0x0b)
		seg = append(seg, wasmtest.ULEB(uint32(len(d.Bytes)))...)
		segments[i] = append(seg, d.Bytes...)
	}
	mem := append([]byte{0}, wasmtest.ULEB(original.MemMinPages)...)
	return wasmtest.Module(
		wasmtest.Section(5, wasmtest.Vec(mem)),
		wasmtest.Section(11, wasmtest.Vec(segments...)),
	)
}

func BenchmarkCOWIntegratedInstantiate(b *testing.B) {
	modules := []struct {
		name        string
		load        func(*testing.B) []byte
		stubImports bool
	}{
		{"yyjson-real", func(b *testing.B) []byte {
			data, err := os.ReadFile(filepath.Join("../..", "corpus/workloads/semantic/yyjson/yyjson.wasm"))
			if err != nil {
				b.Fatal(err)
			}
			return data
		}, false},
		{"php-data-only", func(b *testing.B) []byte { return cowPHPDataOnlyModule(b) }, false},
		{"php-real-stub-imports", func(b *testing.B) []byte {
			data, err := os.ReadFile(filepath.Join("../..", "corpus/workloads/applications/php/php.wasm"))
			if err != nil {
				b.Fatal(err)
			}
			return data
		}, true},
	}
	for _, item := range modules {
		b.Run(item.name, func(b *testing.B) {
			data := item.load(b)
			c, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit).Compile(data)
			if err != nil {
				b.Fatal(err)
			}
			defer c.Close()
			var imports *Imports
			if item.stubImports {
				imports = cowNoopFunctionImports(b, c)
			}
			for _, mode := range []struct {
				name, flag string
			}{{"baseline", "0"}, {"cow", "1"}} {
				b.Run(mode.name, func(b *testing.B) {
					b.Setenv("WAGO_EXPERIMENT_COW_IMAGE", mode.flag)
					warm, err := Instantiate(c, imports)
					if err != nil {
						b.Fatal(err)
					}
					_ = warm.Close()
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						in, err := Instantiate(c, imports)
						if err != nil {
							b.Fatal(err)
						}
						if err := in.Close(); err != nil {
							b.Fatal(err)
						}
					}
					b.ReportMetric(float64(c.CodeSize()), "code-B")
				})
			}
		})
	}
}
