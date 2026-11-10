//go:build amd64

package amd64

import (
	"testing"
)

func adapterGroupBenchInput() ([]byte, []int, []sharedAdapterInfo) {
	const shapes = 128
	const copies = 8
	const size = 24
	code := make([]byte, shapes*copies*size)
	entries := make([]int, shapes*copies)
	infos := make([]sharedAdapterInfo, len(entries))
	for i := range entries {
		entries[i] = size * i
		code[size*i] = byte(i % shapes)
		code[size*i+7] = 0xe8
		infos[i] = sharedAdapterInfo{function: uint32(i), dispOff: 8, endOff: size}
	}
	return code, entries, infos
}

var adapterGroupBenchSink []sharedAdapterGroup
var adapterInfoBenchSink []sharedAdapterInfo
var adapterByteBenchSink int

func BenchmarkPlanSharedAdapterGroups(b *testing.B) {
	code, entries, infos := adapterGroupBenchInput()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		adapterGroupBenchSink, adapterInfoBenchSink, adapterByteBenchSink = planSharedAdaptersAMD64(code, entries, infos)
	}
}
func TestSharedAdapterGroupingManyShapes(t *testing.T) {
	code, entries, infos := adapterGroupBenchInput()
	groups, admitted, total := planSharedAdaptersAMD64(code, entries, infos)
	if len(groups) != 128 || len(admitted) != 1024 || total == 0 {
		t.Fatalf("groups=%d admitted=%d bytes=%d", len(groups), len(admitted), total)
	}
	for i, g := range groups {
		if g.count != 8 || g.templateOff != i*24 {
			t.Fatalf("group %d: %#v", i, g)
		}
	}
	for i, info := range admitted {
		if info.group != uint32(i%128) {
			t.Fatalf("info %d: %#v", i, info)
		}
	}
}
