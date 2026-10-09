package wasm

import (
	"fmt"
	"testing"
)

func equalRecursiveGroups(n int) *Module {
	groups := make([]RecType, 2)
	for i := range groups {
		members := make([]SubType, n)
		for j := range members {
			members[j] = SubType{Final: true, Comp: CompType{Kind: CompFunc, Params: []ValType{I32}}}
		}
		groups[i] = RecType{SubTypes: members}
	}
	return &Module{Types: groups}
}

func TestRecursiveGroupLateMismatchRollsBack(t *testing.T) {
	const members = 128
	for _, member := range []uint32{0, members - 2} {
		m := equalRecursiveGroups(members)
		m.Types[1].SubTypes[members-1].Comp.Params[0] = I64
		v := &moduleValidator{m: m}
		state := make(map[moduleTypePair]uint8)
		a, b := TypeIdx{Index: member}, TypeIdx{Index: member + members}
		if v.typeIdxEquivalentWithState(a, b, state) {
			t.Fatalf("groups with a late mismatch compare equal from member %d", member)
		}
		if len(state) != 0 {
			t.Fatalf("failed comparison retained %d provisional pairs from member %d", len(state), member)
		}
		m.Types[1].SubTypes[members-1].Comp.Params[0] = I32
		if !v.typeIdxEquivalentWithState(a, b, state) {
			t.Fatalf("equal groups compare unequal after rollback from member %d", member)
		}
	}
}

func BenchmarkRecursiveGroupEquivalence(b *testing.B) {
	for _, members := range []int{256, 512, 1024} {
		b.Run(fmt.Sprint(members), func(b *testing.B) {
			m := equalRecursiveGroups(members)
			v := &moduleValidator{m: m}
			a, other := TypeIdx{Index: 0}, TypeIdx{Index: uint32(members)}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if !v.typeIdxEquivalent(a, other) {
					b.Fatal("equal groups compare unequal")
				}
			}
		})
	}
}
