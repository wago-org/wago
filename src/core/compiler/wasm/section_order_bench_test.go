package wasm

import "testing"

var sectionOrderSink uint8

func BenchmarkSectionOrderLookup(b *testing.B) {
	var order uint8
	for i := 0; i < b.N; i++ {
		for id := byte(secType); id <= secTag; id++ {
			var ok bool
			order, ok = lookupSectionOrder(id)
			if !ok {
				b.Fatalf("section %d was not admitted", id)
			}
		}
	}
	sectionOrderSink = order
	b.ReportMetric(13, "lookups/op")
}

func BenchmarkDecodeSectionOrder(b *testing.B) {
	data := module(
		section(secType, 0),
		section(secImport, 0),
		section(secFunction, 0),
		section(secTable, 0),
		section(secMemory, 0),
		section(secTag, 0),
		section(secGlobal, 0),
		section(secExport, 0),
		section(secStart, 0),
		section(secElement, 0),
		section(secDataCount, 0),
		section(secCode, 0),
		section(secData, 0),
	)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := DecodeModuleByteBacked(data); err != nil {
			b.Fatal(err)
		}
	}
}
