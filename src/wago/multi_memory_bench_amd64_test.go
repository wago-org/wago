//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func BenchmarkStagedMultiMemoryLoads(b *testing.B) {
	b.Setenv("WAGO_BOUNDS", "explicit")
	cfg := NewRuntimeConfig()
	features := cfg.frontendFeatures()
	features.MultiMemory = true
	compiled, err := compileWithFrontendFeatures(cfg, localMultiMemoryExecModule(), features)
	if err != nil {
		b.Fatalf("compile: %v", err)
	}
	defer compiled.Close()
	in, err := instantiateCore(compiled, InstantiateOptions{})
	if err != nil {
		b.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	if _, err := in.Invoke("store1", I32(32), I32(7)); err != nil {
		b.Fatalf("initialize memory 1: %v", err)
	}
	for _, name := range []string{"load0", "load1"} {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := in.Invoke(name, I32(32)); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkStagedMultiMemoryImportedContextRebind(b *testing.B) {
	producerCompiled := stagedMultiMemoryCompile(b, officialMultiMemoryProducerModule())
	producer, err := instantiateCore(producerCompiled, InstantiateOptions{})
	if err != nil {
		b.Fatalf("instantiate producer: %v", err)
	}
	defer producer.Close()
	consumerCompiled := stagedMultiMemoryCompile(b, officialMultiMemoryConsumerModule())
	m1, err := producer.ExportedMemory("mem1")
	if err != nil {
		b.Fatal(err)
	}
	m2, err := producer.ExportedMemory("mem2")
	if err != nil {
		b.Fatal(err)
	}
	consumer, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{"M.mem1": m1, "M.mem2": m2}})
	if err != nil {
		b.Fatalf("instantiate consumer: %v", err)
	}
	defer consumer.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := consumer.Invoke("size1"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStagedMultiMemoryExecutableOwnerContextRebind(b *testing.B) {
	producerCompiled := stagedMultiMemoryCompile(b, nativeMultiMemoryProducerModule())
	defer producerCompiled.Close()
	producer, err := instantiateCore(producerCompiled, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer producer.Close()
	m1, err := producer.ExportedMemory("mem1")
	if err != nil {
		b.Fatal(err)
	}
	m2, err := producer.ExportedMemory("mem2")
	if err != nil {
		b.Fatal(err)
	}
	consumerCompiled := stagedMultiMemoryCompile(b, officialMultiMemoryConsumerModule())
	defer consumerCompiled.Close()
	consumer, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{"M.mem1": m1, "M.mem2": m2}})
	if err != nil {
		b.Fatal(err)
	}
	defer consumer.Close()
	for _, tc := range []struct {
		name   string
		in     *Instance
		export string
	}{{"owner", producer, "f"}, {"tenant", consumer, "size1"}} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := tc.in.Invoke(tc.export); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkStagedMultiMemorySIMDLoad(b *testing.B) {
	if !hostSupportsSIMD() {
		b.Skip("host SIMD unavailable")
	}
	body := append([]byte{0x20, 0x00}, indexedSIMDMemOp(0, 4, 0)...)
	body = append(body, 0x0b)
	compiled := stagedMultiMemoryCompile(b, indexedSIMDModule([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.V128}, body))
	in, err := instantiateCore(compiled, InstantiateOptions{})
	if err != nil {
		b.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	m1, err := in.ExportedMemory("m1")
	if err != nil {
		b.Fatalf("export memory 1: %v", err)
	}
	copy(m1.Bytes()[32:], []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := in.Invoke("run", I32(32)); err != nil {
			b.Fatal(err)
		}
	}
}
