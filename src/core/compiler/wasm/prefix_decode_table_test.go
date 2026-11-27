package wasm

import (
	"strings"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/internal/buildfeatures"
)

func TestPrefixLookupTablesStayCompactAndComplete(t *testing.T) {
	tables := []struct {
		name string
		data []InstrKind
		want int
	}{
		{name: "fc-saturating", data: fcSaturating[:], want: 8},
		{name: "fc-wide", data: fcWide[:], want: 4},
		{name: "fb-no-immediate", data: fbNoImm[:], want: 6},
		{name: "fe-memory", data: feMem[:], want: 17},
		{name: "fd-memory", data: fdMem[:], want: 22},
		{name: "fd-lane", data: fdLane[:], want: 14},
		{name: "fd-no-immediate", data: fdNoImm[:], want: 198},
		{name: "fd-relaxed-no-immediate", data: fdRelaxedNoImm[:], want: 20},
	}
	for _, table := range tables {
		count := 0
		for sub, kind := range table.data {
			got, ok := lookupPrefixKind(table.data, uint32(sub))
			if kind == InstrInvalid {
				if ok || got != InstrInvalid {
					t.Errorf("%s subopcode %d invalid lookup = (%v, %v)", table.name, sub, got, ok)
				}
				continue
			}
			count++
			if !ok || got != kind {
				t.Errorf("%s subopcode %d lookup = (%v, %v), want (%v, true)", table.name, sub, got, ok, kind)
			}
		}
		if count != table.want {
			t.Errorf("%s populated entries = %d, want %d", table.name, count, table.want)
		}
		if got, ok := lookupPrefixKind(table.data, uint32(len(table.data))); ok || got != InstrInvalid {
			t.Errorf("%s out-of-range lookup = (%v, %v)", table.name, got, ok)
		}
	}
	if got := unsafe.Sizeof(fcSaturating) + unsafe.Sizeof(fcWide) + unsafe.Sizeof(fbNoImm) + unsafe.Sizeof(feMem) + unsafe.Sizeof(fdMem) + unsafe.Sizeof(fdLane) + unsafe.Sizeof(fdNoImm) + unsafe.Sizeof(fdRelaxedNoImm); got != 956 {
		t.Fatalf("prefix lookup storage = %d bytes, want 956", got)
	}
}

func TestFCPrefixBuildFeatures(t *testing.T) {
	bulkMemory := buildfeatures.BulkMemoryOperations != 0
	referenceTypes := buildfeatures.ReferenceTypes != 0
	bulkTableFeature := "bulk-memory-operations"
	if bulkMemory {
		bulkTableFeature = "reference-types"
	}
	for _, test := range []struct {
		data    []byte
		kind    InstrKind
		feature string
		enabled bool
	}{
		{[]byte{0}, InstrI32TruncSatF32S, "nontrapping-float-to-int-conversion", buildfeatures.NonTrappingFloatToIntConversion != 0},
		{[]byte{8, 0, 0}, InstrMemoryInit, "bulk-memory-operations", bulkMemory},
		{[]byte{9, 0}, InstrDataDrop, "bulk-memory-operations", bulkMemory},
		{[]byte{10, 0, 0}, InstrMemoryCopy, "bulk-memory-operations", bulkMemory},
		{[]byte{11, 0}, InstrMemoryFill, "bulk-memory-operations", bulkMemory},
		{[]byte{12, 0, 0}, InstrTableInit, bulkTableFeature, bulkMemory && referenceTypes},
		{[]byte{13, 0}, InstrElemDrop, bulkTableFeature, bulkMemory && referenceTypes},
		{[]byte{14, 0, 0}, InstrTableCopy, bulkTableFeature, bulkMemory && referenceTypes},
		{[]byte{15, 0}, InstrTableGrow, "reference-types", referenceTypes},
		{[]byte{16, 0}, InstrTableSize, "reference-types", referenceTypes},
		{[]byte{17, 0}, InstrTableFill, "reference-types", referenceTypes},
		{[]byte{19}, InstrI64Add128, "wide-arithmetic", buildfeatures.WideArithmetic != 0},
	} {
		r := reader{data: test.data}
		in, err := decodeFC(&r)
		r = reader{data: test.data}
		var imm InstructionImmediate
		scanErr := classifyFCBytes(&r, &imm, true)
		if !test.enabled {
			for _, got := range []error{err, scanErr} {
				if got == nil || !strings.Contains(got.Error(), test.feature+" is disabled in this build") {
					t.Errorf("subopcode %d: %v", test.data[0], got)
				}
			}
			if test.data[0] >= 8 && test.data[0] <= 17 {
				module := modWithFunc(nil, nil)
				module.Code[0].BodyBytes = append(append([]byte{0xfc}, test.data...), 0x0b)
				checkDisabledInstructionDecode(t, ValidateModule(module), 2, test.feature)
				expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: test.kind}), ErrUnsupportedFeature)
			}
		} else if err != nil || scanErr != nil || in.Kind != test.kind || imm.Kind != test.kind {
			t.Errorf("subopcode %d: decode = %v, %v, classify = %v, %v", test.data[0], in.Kind, err, imm.Kind, scanErr)
		}
	}
}
