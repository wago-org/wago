package wasm

import (
	"encoding/binary"
	"fmt"
	"strings"
	"testing"
)

func singletonTypeModule(n int) []byte {
	payload := binary.AppendUvarint(nil, uint64(n))
	for i := 0; i < n; i++ {
		payload = append(payload, 0x60, 0, 0)
	}
	data := []byte{0, 0x61, 0x73, 0x6d, 1, 0, 0, 0, 1}
	data = binary.AppendUvarint(data, uint64(len(payload)))
	return append(data, payload...)
}
func TestDecodeTypeBudgetAndFlatSingletons(t *testing.T) {
	data := singletonTypeModule(3)
	dm, err := DecodeModuleByteBackedWithLimits(data, ValidationFeatures{}, DecodeLimits{MaxTypes: 3})
	if err != nil {
		t.Fatal(err)
	}
	first := dm.Module.Types[0].SubTypes
	if cap(first) != 1 {
		t.Fatal("singleton exposes neighbors through capacity")
	}
	for _, limits := range []DecodeLimits{{MaxTypes: 2}, {MaxMetadataBytes: 32}} {
		if _, err := DecodeModuleByteBackedWithLimits(data, ValidationFeatures{}, limits); err == nil || !strings.Contains(err.Error(), "limit") {
			t.Fatalf("limits %+v: %v", limits, err)
		}
	}
	// Explicit recursive groups count their members against the same type quota.
	r := reader{data: []byte{1, 0x4e, 2, 0x60, 0, 0, 0x60, 0, 0}, budget: newDecodeBudget(DecodeLimits{MaxTypes: 1})}
	if _, err := decodeTypeSection(&r); err == nil || !strings.Contains(err.Error(), "type count") {
		t.Fatalf("recursive count: %v", err)
	}
}
func TestDecodeBudgetSharedAcrossSections(t *testing.T) {
	data := singletonTypeModule(2)
	limits := DecodeLimits{MaxMetadataBytes: 8192}
	dm, err := DecodeModuleByteBackedWithLimits(data, ValidationFeatures{}, limits)
	if err != nil {
		t.Fatal(err)
	}
	if len(dm.Module.Types) != 2 {
		t.Fatal("type count")
	}
	r := reader{budget: newDecodeBudget(DecodeLimits{MaxMetadataBytes: 16})}
	sub := reader{budget: r.budget}
	if err := r.reserve(12, 1); err != nil {
		t.Fatal(err)
	}
	if err := sub.reserve(8, 1); err == nil {
		t.Fatal("nested reader did not share budget")
	}
	if err := r.reserve(^uint64(0), 8); err == nil {
		t.Fatal("overflow accepted")
	}
}
func BenchmarkDecodeSingletonTypes(b *testing.B) {
	data := singletonTypeModule(100000)
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := DecodeModule(data); err != nil {
			b.Fatal(err)
		}
	}
}

func TestDecodeOpaqueCustomPayloadBudget(t *testing.T) {
	payload := make([]byte, 3<<20)
	payload[len(payload)-1] = 42
	custom := func(name string) []byte {
		data := binary.AppendUvarint(nil, uint64(len(name)))
		data = append(data, name...)
		data = append(data, payload...)
		return section(secCustom, data...)
	}
	data := module(custom(".debug_info"))
	for _, limit := range []uint64{0, uint64(len(payload))*2 + 4096} {
		dm, err := DecodeModuleByteBackedWithLimits(data, ValidationFeatures{}, DecodeLimits{MaxMetadataBytes: limit})
		if err != nil {
			t.Fatalf("opaque payload, limit %d: %v", limit, err)
		}
		if got := dm.Module.Customs[0].Data; len(got) != len(payload) || got[len(got)-1] != 42 {
			t.Fatal("opaque payload changed")
		}
	}
	if _, err := DecodeModuleByteBackedWithLimits(data, ValidationFeatures{}, DecodeLimits{MaxMetadataBytes: uint64(len(payload))}); err == nil || !strings.Contains(err.Error(), "allocation limit") {
		t.Fatalf("owned payload exceeded budget: %v", err)
	}
	for _, name := range []string{"name", branchHintSectionName} {
		if _, err := DecodeModule(module(custom(name))); err == nil || strings.Contains(err.Error(), "allocation limit") {
			t.Fatalf("malformed structured %s payload did not reach strict parsing: %v", name, err)
		}
	}
}

func TestDecodeLargeValidNameWithinBudget(t *testing.T) {
	name := strings.Repeat("a", 3<<20)
	sub := binary.AppendUvarint(nil, uint64(len(name)))
	sub = append(sub, name...)
	payload := []byte{4, 'n', 'a', 'm', 'e', 0}
	payload = binary.AppendUvarint(payload, uint64(len(sub)))
	payload = append(payload, sub...)
	data := module(section(secCustom, payload...))
	dm, err := DecodeModuleByteBackedWithLimits(data, ValidationFeatures{}, DecodeLimits{MaxMetadataBytes: 16 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if dm.Module.NameSec == nil || dm.Module.NameSec.ModuleName == nil || *dm.Module.NameSec.ModuleName != name {
		t.Fatal("module name changed")
	}
	if _, err := DecodeModuleByteBackedWithLimits(data, ValidationFeatures{}, DecodeLimits{MaxMetadataBytes: 8 << 20}); err == nil || !strings.Contains(err.Error(), "allocation limit") {
		t.Fatalf("shared budget: %v", err)
	}
}

func BenchmarkDecodeStructuredCustom(b *testing.B) {
	for _, size := range []int{1024, 64 << 10, 3 << 20} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			sub := binary.AppendUvarint(nil, uint64(size))
			sub = append(sub, strings.Repeat("a", size)...)
			payload := []byte{4, 'n', 'a', 'm', 'e', 0}
			payload = binary.AppendUvarint(payload, uint64(len(sub)))
			payload = append(payload, sub...)
			data := module(section(secCustom, payload...))
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := DecodeModule(data); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func explicitTypeModule(n int) []byte {
	payload := binary.AppendUvarint(nil, uint64(n))
	for i := 0; i < n; i++ {
		payload = append(payload, 0x4e, 1, 0x60, 0, 0)
	}
	return module(section(secType, payload...))
}

func TestExplicitTypesDoNotReserveSingletonSlab(t *testing.T) {
	// This quota covers exact group and subtype storage, but cannot cover an
	// extra subtype per group or the former eight-copy slab reservation.
	data := explicitTypeModule(1000)
	dm, err := DecodeModuleByteBackedWithLimits(data, ValidationFeatures{}, DecodeLimits{MaxMetadataBytes: 400000})
	if err != nil {
		t.Fatal(err)
	}
	if len(dm.Module.Types) != 1000 {
		t.Fatal("wrong type group count")
	}
	for _, group := range dm.Module.Types {
		if len(group.SubTypes) != 1 || cap(group.SubTypes) != 1 {
			t.Fatal("group exposes unrelated types")
		}
	}
}

func BenchmarkDecodeTypeGroups(b *testing.B) {
	for _, n := range []int{1, 1000, 100000} {
		for _, explicit := range []bool{false, true} {
			b.Run(fmt.Sprintf("groups=%d/explicit=%t", n, explicit), func(b *testing.B) {
				data := singletonTypeModule(n)
				if explicit {
					data = explicitTypeModule(n)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if _, err := DecodeModule(data); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
