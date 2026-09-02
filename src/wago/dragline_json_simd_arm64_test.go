//go:build arm64 && !tinygo

package wago

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDraglineJSONSIMDCorpusMatchesRailshot(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "bench", "corpus", "json-as-simd.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	compile := func(config *RuntimeConfig) *Compiled {
		t.Helper()
		compiled, err := Compile(config, source)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { compiled.Close() })
		return compiled
	}
	reference := compile(NewRuntimeConfig().WithCompiler(CompilerRailshot).WithTarget(TargetCompatibility))
	native := []*Compiled{
		compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative)),
		compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative).WithFunctionWorkers(4)),
	}
	cacheConfig := NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative).
		WithFunctionArtifactCache(NewFunctionArtifactCache(4 << 20))
	native = append(native, compile(cacheConfig), compile(cacheConfig))
	instantiate := func(label string, compiled *Compiled) *Instance {
		t.Helper()
		instance, err := Instantiate(compiled, InstantiateOptions{Imports: Imports{
			"env.abort": HostFunc(func(HostModule, []uint64, []uint64) {}),
		}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { instance.Close() })
		if runtime.GOOS == "windows" {
			memory := instance.Memory().UnsafeBytes()
			t.Logf("%s code base=%#x memory=%p stack top=%#x code bytes=%#x", label, instance.base, &memory[0], instance.eng.StackTop(), len(compiled.code))
			t.Logf("%s entry=%v", label, compiled.Entry)
			t.Logf("%s internal=%v", label, compiled.InternalEntry)
			if len(compiled.code) >= 0x2b00 {
				t.Logf("%s code[0x2700:0x2b00]=%x", label, compiled.code[0x2700:0x2b00])
			}
		}
		if _, err := instance.Invoke("_initialize"); err != nil {
			t.Fatal(err)
		}
		return instance
	}
	referenceInstance := instantiate("railshot", reference)
	nativeInstances := make([]*Instance, len(native))
	for i, compiled := range native {
		nativeInstances[i] = instantiate(fmt.Sprintf("dragline-%d", i), compiled)
	}
	for _, export := range []string{"serializeN", "deserializeN"} {
		for _, n := range []int32{0, 1, 2, 200} {
			want, err := referenceInstance.Invoke(export, I32(n))
			if err != nil || len(want) != 1 {
				t.Fatalf("Railshot %s(%d) = %v, %v", export, n, want, err)
			}
			for variant, instance := range nativeInstances {
				got, err := instance.Invoke(export, I32(n))
				if err != nil || len(got) != 1 || got[0] != want[0] {
					t.Errorf("variant %d %s(%d) = %v, %v, want %v", variant, export, n, got, err, want)
				}
			}
		}
	}

	// Exercise every character accepted by the parser's load16_u whitespace
	// loop. Replacing the leading digit in 13 keeps the JSON valid while forcing
	// the loop down its whitespace edge.
	instances := append([]*Instance{referenceInstance}, nativeInstances...)
	needle := []byte{',', 0, '1', 0, '3', 0, ',', 0}
	for _, whitespace := range []uint16{9, 10, 11, 12, 13, 32} {
		offsets := make([][]int, len(instances))
		for variant, instance := range instances {
			memory := instance.Memory().UnsafeBytes()
			for start := 0; start < len(memory); {
				relative := bytes.Index(memory[start:], needle)
				if relative < 0 {
					break
				}
				offset := start + relative + 2
				offsets[variant] = append(offsets[variant], offset)
				binary.LittleEndian.PutUint16(memory[offset:], whitespace)
				start = offset + 2
			}
			if len(offsets[variant]) == 0 {
				t.Fatalf("variant %d encoded JSON is unavailable", variant)
			}
		}
		want, err := referenceInstance.Invoke("deserializeN", I32(1))
		if err != nil || len(want) != 1 {
			t.Fatalf("Railshot whitespace %d = %v, %v", whitespace, want, err)
		}
		for variant, instance := range nativeInstances {
			got, err := instance.Invoke("deserializeN", I32(1))
			if err != nil || len(got) != 1 || got[0] != want[0] {
				t.Errorf("variant %d whitespace %d = %v, %v, want %v", variant, whitespace, got, err, want)
			}
		}
		for variant, instance := range instances {
			memory := instance.Memory().UnsafeBytes()
			for _, offset := range offsets[variant] {
				binary.LittleEndian.PutUint16(memory[offset:], '1')
			}
		}
	}
}
