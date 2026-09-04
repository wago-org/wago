//go:build linux && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestAssociativeTreeCover(t *testing.T) {
	// (a + b) + (c + d): the balanced tree needs three registers normally.
	body := []byte{
		0x00,
		0x20, 0x00, 0x20, 0x01, 0x6a,
		0x20, 0x02, 0x20, 0x03, 0x6a,
		0x6a,
		0x41, 0x30, 0x46, // == 48; comparison materializes the add tree without a destination hint.
		0x0b,
	}
	m := mod1(t,
		[]wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32},
		[]wasm.ValType{wasm.I32}, body)

	saved := associativeTreeEnabled
	defer func() { associativeTreeEnabled = saved }()

	associativeTreeEnabled = true
	on := compileWithStats(t, m, false).Funcs[0]
	if got := runAmd64(t, m, 7, 11, 13, 17); got != 1 {
		t.Fatalf("enabled result = %d, want 1", got)
	}
	if hits := on.Peephole["assoc-tree"]; hits != 1 {
		t.Fatalf("assoc-tree = %d, want 1 (all: %v)", hits, on.Peephole)
	}

	associativeTreeEnabled = false
	off := compileWithStats(t, m, false).Funcs[0]
	if got := runAmd64(t, m, 7, 11, 13, 17); got != 1 {
		t.Fatalf("disabled result = %d, want 1", got)
	}
	if hits := off.Peephole["assoc-tree-candidate"]; hits != 1 {
		t.Fatalf("assoc-tree-candidate = %d, want 1 (all: %v)", hits, off.Peephole)
	}
	if hits := off.Peephole["assoc-tree"]; hits != 0 {
		t.Fatalf("disabled assoc-tree = %d, want 0", hits)
	}
}

func TestAssociativeTreeCoverBeyondEightLeaves(t *testing.T) {
	// A balanced sixteen-leaf tree fits under Valent's height bound but exceeded
	// the old eight-element collection array. The generalized cover should select
	// the whole root, not split it into smaller independently materialized trees.
	body := []byte{0x00}
	var appendSum func(lo, hi byte)
	appendSum = func(lo, hi byte) {
		if hi-lo == 1 {
			body = append(body, 0x20, lo) // local.get lo
			return
		}
		mid := lo + (hi-lo)/2
		appendSum(lo, mid)
		appendSum(mid, hi)
		body = append(body, 0x6a) // i32.add
	}
	appendSum(0, 16)
	body = append(body, 0x41, 0x10, 0x46, 0x0b) // == 16; end
	params := make([]wasm.ValType, 16)
	args := make([]int32, 16)
	for i := range params {
		params[i] = wasm.I32
		args[i] = 1
	}
	m := mod1(t, params, []wasm.ValType{wasm.I32}, body)

	saved := associativeTreeEnabled
	defer func() { associativeTreeEnabled = saved }()
	associativeTreeEnabled = true
	stats := compileWithStats(t, m, false).Funcs[0]
	if got := runAmd64(t, m, args...); got != 1 {
		t.Fatalf("result = %d, want 1", got)
	}
	if hits := stats.Peephole["assoc-tree"]; hits != 1 {
		t.Fatalf("assoc-tree = %d, want one whole-tree cover (all: %v)", hits, stats.Peephole)
	}
}

func TestAssociativeTreeCoverDestination(t *testing.T) {
	tests := []struct {
		name   string
		locals []byte
		args   []int32
		want   int32
		hit    bool
	}{
		{
			name:   "no-alias",
			locals: []byte{1, 2, 3, 4, 5, 6, 7, 8},
			args:   []int32{99, 1, 2, 3, 4, 5, 6, 7, 8},
			want:   36,
			hit:    true,
		},
		{
			name:   "one-destination-input",
			locals: []byte{0, 1, 2, 3, 4, 5, 6, 7},
			args:   []int32{1, 2, 3, 4, 5, 6, 7, 8, 0},
			want:   36,
			hit:    true,
		},
		{
			name:   "repeated-destination-input",
			locals: []byte{0, 1, 2, 3, 0, 4, 5, 6},
			args:   []int32{1, 2, 3, 4, 5, 6, 7, 0, 0},
			want:   29,
			hit:    true,
		},
	}

	saved := associativeTreeEnabled
	defer func() { associativeTreeEnabled = saved }()
	associativeTreeEnabled = true
	params := make([]wasm.ValType, 9)
	for i := range params {
		params[i] = wasm.I32
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := []byte{0x00}
			var appendSum func([]byte)
			appendSum = func(locals []byte) {
				if len(locals) == 1 {
					body = append(body, 0x20, locals[0])
					return
				}
				mid := len(locals) / 2
				appendSum(locals[:mid])
				appendSum(locals[mid:])
				body = append(body, 0x6a)
			}
			appendSum(tt.locals)
			body = append(body, 0x21, 0x00, 0x20, 0x00, 0x0b)
			m := mod1(t, params, []wasm.ValType{wasm.I32}, body)
			stats := compileWithStats(t, m, false).Funcs[0]
			if got := runAmd64(t, m, tt.args...); got != tt.want {
				t.Fatalf("result = %d, want %d", got, tt.want)
			}
			if got := stats.Peephole["assoc-tree-dest"] != 0; got != tt.hit {
				t.Fatalf("assoc-tree-dest hit = %v, want %v (all: %v)", got, tt.hit, stats.Peephole)
			}
		})
	}
}

func TestAssociativeTreeCoverNestedRepeatedDestination(t *testing.T) {
	// Two flattened add leaves read the destination inside shift subtrees. Keep
	// one old-value copy alive while the accumulator overwrites local 0.
	body := []byte{0x00}
	appendLeaf := func(i int) {
		switch i {
		case 0:
			body = append(body, 0x20, 0x00, 0x41, 0x01, 0x74) // local0 << 1
		case 4:
			body = append(body, 0x20, 0x00, 0x41, 0x02, 0x74) // local0 << 2
		default:
			body = append(body, 0x20, byte(i))
		}
	}
	var appendSum func(lo, hi int)
	appendSum = func(lo, hi int) {
		if hi-lo == 1 {
			appendLeaf(lo)
			return
		}
		mid := lo + (hi-lo)/2
		appendSum(lo, mid)
		appendSum(mid, hi)
		body = append(body, 0x6a)
	}
	appendSum(0, 8)
	body = append(body, 0x21, 0x00, 0x20, 0x00, 0x0b)
	params := make([]wasm.ValType, 8)
	for i := range params {
		params[i] = wasm.I32
	}
	m := mod1(t, params, []wasm.ValType{wasm.I32}, body)
	saved := associativeTreeEnabled
	defer func() { associativeTreeEnabled = saved }()
	associativeTreeEnabled = true
	stats := compileWithStats(t, m, false).Funcs[0]
	if got := runAmd64(t, m, 3, 1, 2, 3, 4, 5, 6, 0); got != 35 {
		t.Fatalf("result = %d, want 35", got)
	}
	if hits := stats.Peephole["assoc-tree-dest-repeat"]; hits != 1 {
		t.Fatalf("assoc-tree-dest-repeat = %d, want 1 (all: %v)", hits, stats.Peephole)
	}
	associativeTreeEnabled = false
	off := compileWithStats(t, m, false).Funcs[0]
	if got := runAmd64(t, m, 3, 1, 2, 3, 4, 5, 6, 0); got != 35 {
		t.Fatalf("disabled result = %d, want 35", got)
	}
	if hits := off.Peephole["assoc-tree-dest-repeat"]; hits != 0 {
		t.Fatalf("disabled assoc-tree-dest-repeat = %d, want 0", hits)
	}
}

func TestAssociativeTreeCoverRestoresRepeatedDestinationPin(t *testing.T) {
	// The first local 0 read seeds the destination accumulator. The compare's
	// rewritten local 0 read temporarily pins and unpins the saved alias copy;
	// preserve that outer pin across the compare and the following clz allocation
	// so the final rewritten local 0 read still observes the original value.
	leaves := [][]byte{
		{0x20, 0x00},                   // local.get 0 (accumulator seed)
		{0x20, 0x00, 0x41, 0x07, 0x46}, // local.get 0 == 7
		{0x20, 0x01, 0x67},             // i32.clz(local.get 1), allocates a GPR
		{0x20, 0x02},
		{0x20, 0x03},
		{0x20, 0x04},
		{0x20, 0x00}, // later rewritten destination read
		{0x20, 0x05},
	}
	body := []byte{0x00}
	var appendSum func(lo, hi int)
	appendSum = func(lo, hi int) {
		if hi-lo == 1 {
			body = append(body, leaves[lo]...)
			return
		}
		mid := lo + (hi-lo)/2
		appendSum(lo, mid)
		appendSum(mid, hi)
		body = append(body, 0x6a)
	}
	appendSum(0, len(leaves))
	body = append(body, 0x21, 0x00, 0x20, 0x00, 0x0b)
	params := make([]wasm.ValType, 6)
	for i := range params {
		params[i] = wasm.I32
	}
	m := mod1(t, params, []wasm.ValType{wasm.I32}, body)

	saved := associativeTreeEnabled
	defer func() { associativeTreeEnabled = saved }()
	associativeTreeEnabled = true
	stats := compileWithStats(t, m, false).Funcs[0]
	if got := runAmd64(t, m, 7, 8, 2, 3, 4, 5); got != 57 {
		t.Fatalf("result = %d, want 57", got)
	}
	if hits := stats.Peephole["assoc-tree-dest-repeat"]; hits != 1 {
		t.Fatalf("assoc-tree-dest-repeat = %d, want 1 (all: %v)", hits, stats.Peephole)
	}
}

func TestTreeAccumulatorSafety(t *testing.T) {
	leaf := func(kind storageKind) *elem {
		return testValueElem(storage{kind: kind, typ: mtI32})
	}
	constantShift := testDeferredElem(opShl, mtI32, leaf(stReg), leaf(stConst))
	if !treeAccumulatorSafe(constantShift) {
		t.Fatal("constant shift should honor a live accumulator")
	}
	variableShift := testDeferredElem(opShl, mtI32, leaf(stReg), leaf(stReg))
	if treeAccumulatorSafe(variableShift) {
		t.Fatal("variable shift can evict an RCX accumulator")
	}
}

func TestTreeRegReplaceable(t *testing.T) {
	borrowed := testValueElem(storage{kind: stLocalReg, reg: RAX})
	owned := testValueElem(storage{kind: stReg, reg: RAX})
	other := testValueElem(storage{kind: stReg, reg: RCX})
	if !treeRegReplaceable(borrowed, RAX) {
		t.Fatal("borrowed local register should be replaceable")
	}
	if treeRegReplaceable(owned, RAX) {
		t.Fatal("owned register must preserve allocator ownership")
	}
	if !treeRegReplaceable(other, RAX) {
		t.Fatal("unrelated owned register should not block replacement")
	}
}
