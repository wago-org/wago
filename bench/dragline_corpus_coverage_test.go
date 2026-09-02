package wagobench

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/wago-org/wago"
)

func TestDraglineRaytraceDifferential(t *testing.T) {
	moduleBytes, err := os.ReadFile(filepath.Join(corpusDir, "raytrace.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	compile := func(engine wago.CompilerEngine) *wago.Compiled {
		compiled, err := wago.NewRuntimeConfig().WithCompiler(engine).Compile(moduleBytes)
		if err != nil {
			t.Fatalf("%s compile: %v", engine, err)
		}
		t.Cleanup(func() { compiled.Close() })
		return compiled
	}
	wantCode, gotCode := compile(wago.CompilerRailshot), compile(wago.CompilerDragline)
	for n := int32(0); n <= 48; n++ {
		want, err := wago.Instantiate(wantCode, wago.InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		got, err := wago.Instantiate(gotCode, wago.InstantiateOptions{})
		if err != nil {
			want.Close()
			t.Fatal(err)
		}
		wantResult, wantErr := want.Invoke("render", wago.I32(n))
		gotResult, gotErr := got.Invoke("render", wago.I32(n))
		want.Close()
		got.Close()
		if (wantErr == nil) != (gotErr == nil) || !slices.Equal(gotResult, wantResult) {
			t.Fatalf("render(%d): Dragline=(%#x,%v), Railshot=(%#x,%v)", n, gotResult, gotErr, wantResult, wantErr)
		}
	}
}

func TestDraglineMemoryTreeDifferential(t *testing.T) {
	moduleBytes, err := os.ReadFile(filepath.Join(corpusDir, "memory_tree.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	compile := func(engine wago.CompilerEngine) *wago.Compiled {
		compiled, err := wago.NewRuntimeConfig().WithCompiler(engine).Compile(moduleBytes)
		if err != nil {
			t.Fatalf("%s compile: %v", engine, err)
		}
		t.Cleanup(func() { compiled.Close() })
		return compiled
	}
	wantCode, gotCode := compile(wago.CompilerRailshot), compile(wago.CompilerDragline)
	want, err := wago.Instantiate(wantCode, wago.InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer want.Close()
	got, err := wago.Instantiate(gotCode, wago.InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer got.Close()
	for _, depth := range []int32{0, 1, 2, 8} {
		wantResult, wantErr := want.Invoke("run", wago.I32(depth), wago.I32(24))
		gotResult, gotErr := got.Invoke("run", wago.I32(depth), wago.I32(24))
		if (wantErr == nil) != (gotErr == nil) || !slices.Equal(gotResult, wantResult) {
			t.Fatalf("depth %d: Dragline=(%#x,%v), Railshot=(%#x,%v)", depth, gotResult, gotErr, wantResult, wantErr)
		}
	}
}

func TestDraglineMulhiDifferential(t *testing.T) {
	moduleBytes, err := os.ReadFile(filepath.Join(corpusDir, "xjb-mulhi.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	compile := func(engine wago.CompilerEngine) *wago.Compiled {
		compiled, err := wago.NewRuntimeConfig().WithCompiler(engine).Compile(moduleBytes)
		if err != nil {
			t.Fatalf("%s compile: %v", engine, err)
		}
		t.Cleanup(func() { compiled.Close() })
		return compiled
	}
	wantCode, gotCode := compile(wago.CompilerRailshot), compile(wago.CompilerDragline)
	want, err := wago.Instantiate(wantCode, wago.InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer want.Close()
	got, err := wago.Instantiate(gotCode, wago.InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer got.Close()
	for _, args := range [][2]int32{{0x12345678, -0x65432110}, {-1, -1}, {1, 1}} {
		wantResult, wantErr := want.Invoke("mulhi", wago.I32(args[0]), wago.I32(args[1]))
		gotResult, gotErr := got.Invoke("mulhi", wago.I32(args[0]), wago.I32(args[1]))
		if (wantErr == nil) != (gotErr == nil) || !slices.Equal(gotResult, wantResult) {
			t.Fatalf("mulhi%v: Dragline=(%#x,%v), Railshot=(%#x,%v)", args, gotResult, gotErr, wantResult, wantErr)
		}
	}
}

// TestDraglineCorpusCoverage identifies which curated non-ISA corpus modules
// execute through strict Dragline today. Unsupported modules are reported, not
// treated as fallback successes. Any module Dragline admits must instantiate
// and agree with an independently compiled Railshot instance.
//
//	WAGO_DRAGLINE_CORPUS_COVERAGE=1 go test -run TestDraglineCorpusCoverage -v
func TestDraglineCorpusCoverage(t *testing.T) {
	if os.Getenv("WAGO_DRAGLINE_CORPUS_COVERAGE") != "1" {
		t.Skip("set WAGO_DRAGLINE_CORPUS_COVERAGE=1 to run curated corpus coverage")
	}
	modules := readManifest(t, "manifest.json")
	runnable := 0
	compileOnly := 0
	for _, module := range modules {
		if !module.avail {
			continue
		}
		dragline, err := wago.NewRuntimeConfig().WithCompiler(wago.CompilerDragline).Compile(module.bytes)
		if err != nil {
			t.Logf("UNSUPPORTED %s: %v", module.File, err)
			continue
		}
		railshot, err := wago.NewRuntimeConfig().WithCompiler(wago.CompilerRailshot).Compile(module.bytes)
		if err != nil {
			dragline.Close()
			t.Fatalf("Railshot compile %s after Dragline admitted it: %v", module.File, err)
		}
		if len(module.Exec) == 0 && module.Init == "" {
			railshot.Close()
			dragline.Close()
			compileOnly++
			t.Logf("COMPILES %s", module.File)
			continue
		}
		runCorpusPair(t, module, railshot, dragline)
		railshot.Close()
		dragline.Close()
		runnable++
		t.Logf("RUNNABLE %s", module.File)
	}
	t.Logf("Dragline curated corpus: runnable=%d compile-only=%d available=%d", runnable, compileOnly, len(modules))
}

func TestDraglineGlobalsClosedSumDifferential(t *testing.T) {
	var module corpusModule
	for _, candidate := range readManifest(t, "manifest.json") {
		if candidate.File == "globals.wasm" {
			module = candidate
			break
		}
	}
	if len(module.bytes) == 0 {
		t.Fatal("globals.wasm is missing from the corpus")
	}
	compile := func(compiler wago.CompilerEngine) *wago.Instance {
		t.Helper()
		compiled, err := wago.NewRuntimeConfig().WithCompiler(compiler).WithTarget(wago.TargetNative).Compile(module.bytes)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { compiled.Close() })
		instance, err := wago.Instantiate(compiled, wago.InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = instance.Close() })
		return instance
	}
	want, got := compile(wago.CompilerRailshot), compile(wago.CompilerDragline)
	for _, n := range []uint32{0, 1, 2, 17, 2000, 65535} {
		wantResult, wantErr := want.Invoke("accumulate", wago.I32(int32(n)))
		gotResult, gotErr := got.Invoke("accumulate", wago.I32(int32(n)))
		if (wantErr == nil) != (gotErr == nil) || !slices.Equal(gotResult, wantResult) {
			t.Fatalf("accumulate(%d): Dragline=(%#x,%v), Railshot=(%#x,%v)", n, gotResult, gotErr, wantResult, wantErr)
		}
	}
}

// TestDraglineSIMDCorpusDifferential locks the bounded real-world SIMD subset
// to independent Railshot results on every architecture that can execute the
// generated native code. Unlike the broad coverage inventory, this is a normal
// correctness gate and must never turn an unsupported module into a skip.
func TestDraglineSIMDCorpusDifferential(t *testing.T) {
	want := map[string]bool{
		"json-as-simd.wasm":  true,
		"blake-as-simd.wasm": true,
		"utf-as-simd.wasm":   true,
	}
	for _, module := range readManifest(t, "manifest.json") {
		if !want[module.File] {
			continue
		}
		dragline, err := wago.NewRuntimeConfig().WithCompiler(wago.CompilerDragline).Compile(module.bytes)
		if err != nil {
			t.Fatalf("Dragline compile %s: %v", module.File, err)
		}
		railshot, err := wago.NewRuntimeConfig().WithCompiler(wago.CompilerRailshot).Compile(module.bytes)
		if err != nil {
			dragline.Close()
			t.Fatalf("Railshot compile %s: %v", module.File, err)
		}
		runCorpusPair(t, module, railshot, dragline)
		railshot.Close()
		dragline.Close()
		delete(want, module.File)
	}
	if len(want) != 0 {
		t.Fatalf("SIMD corpus entries unavailable: %v", want)
	}
}

func BenchmarkRailshotDraglineSIMDCorpusExec(b *testing.B) {
	want := map[string]bool{"json-as-simd.wasm": true, "blake-as-simd.wasm": true, "utf-as-simd.wasm": true}
	for _, module := range readManifest(b, "manifest.json") {
		if !want[module.File] {
			continue
		}
		for _, entry := range module.Exec {
			for _, compiler := range []wago.CompilerEngine{wago.CompilerRailshot, wago.CompilerDragline} {
				compiler := compiler
				b.Run(module.name()+"."+entry.Export+"/"+compiler.String(), func(b *testing.B) {
					compiled, err := wago.NewRuntimeConfig().WithCompiler(compiler).Compile(module.bytes)
					if err != nil {
						b.Fatal(err)
					}
					defer compiled.Close()
					instance, err := wago.Instantiate(compiled, wago.InstantiateOptions{Imports: wago.Imports{
						"env.abort": wago.HostFunc(func(wago.HostModule, []uint64, []uint64) {}),
					}})
					if err != nil {
						b.Fatal(err)
					}
					defer instance.Close()
					if module.Init != "" {
						if _, err := instance.Invoke(module.Init); err != nil {
							b.Fatal(err)
						}
					}
					args := make([]uint64, len(entry.Args))
					for i, arg := range entry.Args {
						args[i] = wago.I32(arg)
					}
					fn, err := instance.PrepareFunction(entry.Export)
					if err != nil {
						b.Fatal(err)
					}
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						if _, err := invokePrepared(fn, args); err != nil {
							b.Fatal(err)
						}
					}
				})
			}
		}
	}
}

func runCorpusPair(t *testing.T, module corpusModule, railshot, dragline *wago.Compiled) {
	t.Helper()
	instantiate := func(compiled *wago.Compiled) *wago.Instance {
		instance, err := wago.Instantiate(compiled, wago.InstantiateOptions{Imports: wago.Imports{
			"env.abort": wago.HostFunc(func(wago.HostModule, []uint64, []uint64) {}),
		}})
		if err != nil {
			t.Fatalf("instantiate %s with %s: %v", module.File, compiled.Compiler(), err)
		}
		return instance
	}
	wantInstance := instantiate(railshot)
	defer wantInstance.Close()
	gotInstance := instantiate(dragline)
	defer gotInstance.Close()
	if module.Init != "" {
		if _, err := wantInstance.Invoke(module.Init); err != nil {
			t.Fatalf("Railshot %s init: %v", module.File, err)
		}
		if _, err := gotInstance.Invoke(module.Init); err != nil {
			t.Fatalf("Dragline %s init: %v", module.File, err)
		}
	}
	for _, entry := range module.Exec {
		args := make([]uint64, len(entry.Args))
		for i, arg := range entry.Args {
			args[i] = wago.I32(arg)
		}
		want, wantErr := wantInstance.Invoke(entry.Export, args...)
		got, gotErr := gotInstance.Invoke(entry.Export, args...)
		if (wantErr == nil) != (gotErr == nil) || !slices.Equal(got, want) {
			t.Fatalf("%s.%s%v: Dragline=(%#x,%v), Railshot=(%#x,%v)", module.File, entry.Export, entry.Args, got, gotErr, want, wantErr)
		}
	}
}
