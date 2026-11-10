package wasm

import (
	"fmt"
	"testing"
)

var importRepCountSink int

func appendImportRepName(dst []byte, name string) []byte {
	dst = appendTypeRepU32(dst, uint32(len(name)))
	return append(dst, name...)
}

func appendImportRepEntry(dst []byte, kind ExternKind) []byte {
	dst = appendImportRepName(dst, "env")
	dst = appendImportRepName(dst, "x")
	dst = append(dst, byte(kind))
	switch kind {
	case ExternFunc:
		dst = append(dst, 0)
	case ExternTable:
		// funcref, min=0, max=1
		dst = append(dst, byte(HeapFunc), 1, 0, 1)
	case ExternMem:
		// memory32, min=0, max=1
		dst = append(dst, 1, 0, 1)
	case ExternGlobal:
		dst = append(dst, byte(NumI32), byte(Const))
	case ExternTag:
		// exception attribute 0, function type 0
		dst = append(dst, 0, 0)
	default:
		panic("unsupported synthetic import kind")
	}
	return dst
}

func syntheticImportModuleBytes(kindName string, imports int) []byte {
	// One empty function type supports both function and tag imports.
	typePayload := []byte{1, 0x60, 0, 0}
	module := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00, secType}
	module = appendTypeRepU32(module, uint32(len(typePayload)))
	module = append(module, typePayload...)

	payload := appendTypeRepU32(nil, uint32(imports))
	for i := 0; i < imports; i++ {
		var kind ExternKind
		switch kindName {
		case "functions":
			kind = ExternFunc
		case "globals":
			kind = ExternGlobal
		case "tables":
			kind = ExternTable
		case "memories":
			kind = ExternMem
		case "tags":
			kind = ExternTag
		case "mixed":
			kind = ExternKind(i % 5)
		default:
			panic("unsupported synthetic import shape")
		}
		payload = appendImportRepEntry(payload, kind)
	}
	module = append(module, secImport)
	module = appendTypeRepU32(module, uint32(len(payload)))
	return append(module, payload...)
}

func benchmarkImportShapes(b *testing.B, maximumMemoryImports int, fn func(*testing.B, []byte)) {
	for _, kind := range []string{"functions", "globals", "tables", "memories", "tags", "mixed"} {
		counts := []int{10, 100, 1000, 10000}
		if kind == "memories" && maximumMemoryImports < counts[len(counts)-1] {
			counts[len(counts)-1] = maximumMemoryImports
		}
		for _, imports := range counts {
			data := syntheticImportModuleBytes(kind, imports)
			b.Run(fmt.Sprintf("kind=%s/imports=%d", kind, imports), func(b *testing.B) {
				fn(b, data)
			})
		}
	}
}

func syntheticNamedFunctionImports(imports int, moduleName, fieldName func(int) string) []byte {
	typePayload := []byte{1, 0x60, 0, 0}
	module := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00, secType}
	module = appendTypeRepU32(module, uint32(len(typePayload)))
	module = append(module, typePayload...)
	payload := appendTypeRepU32(nil, uint32(imports))
	for i := 0; i < imports; i++ {
		payload = appendImportRepName(payload, moduleName(i))
		payload = appendImportRepName(payload, fieldName(i))
		payload = append(payload, byte(ExternFunc), 0)
	}
	module = append(module, secImport)
	module = appendTypeRepU32(module, uint32(len(payload)))
	return append(module, payload...)
}

func BenchmarkImportNameDecode(b *testing.B) {
	const imports = 1000
	cases := []struct {
		name string
		data []byte
	}{
		{"repeated-module", syntheticNamedFunctionImports(imports, func(int) string { return "env" }, func(i int) string { return fmt.Sprintf("field-%04d", i) })},
		{"repeated-field", syntheticNamedFunctionImports(imports, func(i int) string { return fmt.Sprintf("module-%04d", i) }, func(int) string { return "field" })},
		{"all-unique", syntheticNamedFunctionImports(imports, func(i int) string { return fmt.Sprintf("module-%04d", i) }, func(i int) string { return fmt.Sprintf("field-%04d", i) })},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				m, err := DecodeModule(tc.data)
				if err != nil {
					b.Fatal(err)
				}
				typeRepModuleSink = m
			}
		})
	}
}

func BenchmarkImportMetadataDecode(b *testing.B) {
	benchmarkImportShapes(b, 10000, func(b *testing.B, data []byte) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			m, err := DecodeModule(data)
			if err != nil {
				b.Fatal(err)
			}
			typeRepModuleSink = m
		}
	})
}

func BenchmarkImportMetadataValidate(b *testing.B) {
	benchmarkImportShapes(b, int(MaximumMemoriesPerModule), func(b *testing.B, data []byte) {
		m, err := DecodeModule(data)
		if err != nil {
			b.Fatal(err)
		}
		features := ValidationFeatures{MultiMemory: true}
		limits := ValidationLimits{MaxMemoriesPerModule: MaximumMemoriesPerModule}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if err := ValidateModuleWithConfig(m, features, 1, limits); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkImportMetadataIterate(b *testing.B) {
	benchmarkImportShapes(b, 10000, func(b *testing.B, data []byte) {
		m, err := DecodeModule(data)
		if err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			n := 0
			for kind := ExternFunc; kind <= ExternTag; kind++ {
				n += m.importCount(kind)
			}
			importRepCountSink = n
		}
	})
}
