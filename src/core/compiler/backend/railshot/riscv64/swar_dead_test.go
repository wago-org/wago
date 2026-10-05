//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type swarDeadCase struct {
	function fixtureFunc
	kernel   []byte
	outputs  int
	memory   bool
}

// Each real frontend-validated fixture supplies the live result. Repeat only
// its opcode after return, with no operand-producing instructions, to exercise
// polymorphic input synthesis while proving that no ghost code/frame is added.
func swarDeadCases() []swarDeadCase {
	var cases []swarDeadCase
	add := func(f fixtureFunc, kernel []byte, outputs int, memory bool) {
		cases = append(cases, swarDeadCase{f, kernel, outputs, memory})
	}
	vector := func(kind uint32, unary, scalar bool) {
		f := swarArithmeticFixture(kind)
		if unary {
			f = swarUnaryFixture(kind, scalar)
		}
		add(f, uleb([]byte{0xfd}, kind), 1, false)
	}
	for _, c := range swarScalarIntegerCases() {
		f, _ := swarScalarIntegerFixture(c)
		add(f, []byte{c.op}, 1, false)
	}
	for _, c := range swarScalarFloatCases() {
		f, _ := swarScalarFloatFixture(c)
		kernel := []byte{c.op}
		if c.op == 0xfc {
			kernel = append(kernel, c.sub)
		}
		add(f, kernel, 1, false)
	}
	for kind := uint32(35); kind <= 76; kind++ {
		vector(kind, false, false)
	}
	for kind := uint32(214); kind <= 219; kind++ {
		vector(kind, false, false)
	}
	for _, kinds := range [][]uint32{{78, 79, 80, 81, 110, 113, 142, 145, 174, 177, 206, 209, 130, 186}, swarProductKinds, swarSaturationKinds, swarPackKinds, swarFloatMinMaxKinds} {
		for _, kind := range kinds {
			vector(kind, false, false)
		}
	}
	for _, kinds := range [][]uint32{{77, 98}, swarWidenKinds, swarFloatRoundKinds, swarFloatConvertKinds} {
		for _, kind := range kinds {
			vector(kind, true, false)
		}
	}
	vector(83, true, true)
	for _, base := range []uint32{96, 128, 160, 192} {
		for _, offset := range []uint32{0, 1, 3, 4} {
			vector(base+offset, true, offset >= 3)
		}
	}
	for _, kind := range swarFloatMathKinds {
		vector(kind, kind == 224 || kind == 225 || kind == 227 || kind == 236 || kind == 237 || kind == 239, false)
	}
	for _, base := range []uint32{107, 139, 171, 203} {
		for offset := uint32(0); offset < 3; offset++ {
			kind := base + offset
			add(swarShiftFixture(kind), uleb([]byte{0xfd}, kind), 1, false)
		}
	}
	for kind := uint32(15); kind <= 34; kind++ {
		if kind >= 18 && kind <= 20 || kind >= 29 {
			f := swarWideLaneFixture(kind, 0)
			kernel := uleb([]byte{0xfd}, kind)
			if kind >= 29 {
				kernel = append(kernel, 0)
			}
			add(f, kernel, 1, false)
		} else {
			f := swarLaneFixture(kind, 0)
			kernel := uleb([]byte{0xfd}, kind)
			if kind >= 21 {
				kernel = append(kernel, 0)
			}
			add(f, kernel, 1, false)
		}
	}
	for kind := uint32(256); kind <= 275; kind++ {
		add(swarRelaxedFixture(kind), uleb([]byte{0xfd}, kind), 1, false)
	}
	add(fixtureFunc{params: []byte{0x7b, 0x7b, 0x7b}, result: 0x7b, body: []byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfd, 82, 0x0b}}, []byte{0xfd, 82}, 1, false)
	for _, shuffle := range []bool{false, true} {
		f := swarPermutationFixture(shuffle, [16]byte{31, 0, 16, 1})
		add(f, f.body[4:len(f.body)-1], 1, false)
	}
	for _, store := range []bool{false, true} {
		f, kind, outputs := swarMemoryFixture(store, 0, 0), byte(0), 1
		if store {
			kind, outputs = 11, 0
		}
		add(f, []byte{0xfd, kind, 0, 0}, outputs, true)
	}
	for _, kind := range append([]uint32{7, 8, 9, 10}, swarPartialLoadKinds...) {
		kernel := append(uleb([]byte{0xfd}, kind), 0, 0)
		add(swarPartialLoadFixture(kind, 0, 0), kernel, 1, true)
	}
	for kind := uint32(84); kind <= 91; kind++ {
		outputs := 1
		if kind >= 88 {
			outputs = 0
		}
		add(swarLaneMemoryFixture(kind, 0, 0, 0), []byte{0xfd, byte(kind), 0, 0, 0}, outputs, true)
	}
	identity := fixtureFunc{params: []byte{0x7b}, result: 0x7b, body: []byte{0x20, 0, 0x0b}}
	for _, kernel := range [][]byte{{0x1b}, {0x1c, 1, 0x7b}, {0x22, 0}, {0x20, 0}, swarLiteral(1, 0x7ff0000000000001)} {
		add(identity, kernel, 1, false)
	}
	for _, kernel := range [][]byte{{0x1a}, {0x21, 0}} {
		add(identity, kernel, 0, false)
	}
	return cases
}

func swarDeadFixture(c swarDeadCase, ghost bool) fixtureFunc {
	f := c.function
	f.body = append(append([]byte(nil), f.body[:len(f.body)-1]...), 0x0f)
	if ghost {
		f.body = append(f.body, c.kernel...)
		f.body = append(f.body, bytes.Repeat([]byte{0x1a}, c.outputs)...)
	}
	f.body = append(f.body, 0x0b)
	return f
}

func TestSWARPolymorphicFamiliesNoGhostCodeOrFrame(t *testing.T) {
	for index, c := range swarDeadCases() {
		var baseline []byte
		var baselineInfo FunctionInfo
		for _, ghost := range []bool{false, true} {
			f := swarDeadFixture(c, ghost)
			var fn *wasm.Func
			var ft *wasm.CompType
			var memory *wasm.MemType
			if c.memory {
				fn, ft, memory = swarValidatedMemoryFunction(t, f)
			} else {
				fn, ft = swarValidatedFunction(t, f)
			}
			code, info, err := compileSWARPairsWithMemory(fn, ft, memory, emissionMode{production: true, interruptible: true})
			if len(code) == 0 || err != nil {
				t.Fatal("polymorphic supported family", index, c.kernel, ghost, err)
			}
			if !ghost {
				baseline, baselineInfo = code, info
			} else if !bytes.Equal(code, baseline) || info.FrameBytes != baselineInfo.FrameBytes || info.SpillSlots != baselineInfo.SpillSlots {
				t.Fatal("dead opcode emitted code or enlarged frame", index, c.kernel, len(code), len(baseline), info, baselineInfo)
			}
		}
	}
}

func swarDeadOuterJoinFixture(ghost bool) fixtureFunc {
	f := fixtureFunc{params: []byte{0x7b, 0x7b}, result: 0x7b}
	f.body = []byte{2, 0x7b, 2, 0x7b, 0x20, 0, 0x0c, 1}
	if ghost {
		// These typed values remain virtual even though the outward branch
		// revives execution at the outer join below.
		for i := 0; i < 128; i++ {
			f.body = append(f.body, swarLiteral(uint64(i), ^uint64(i))...)
		}
		f.body = append(f.body, bytes.Repeat([]byte{0x1a}, 128)...)
	}
	f.body = append(f.body, 0x0b, 0x0b, 0x20, 1, 0xfd, 81, 0x0b)
	return f
}

func TestSWARDeadOuterBranchJoinResumesEmission(t *testing.T) {
	var baseline []byte
	var baselineInfo FunctionInfo
	for _, ghost := range []bool{false, true} {
		fn, ft := swarValidatedFunction(t, swarDeadOuterJoinFixture(ghost))
		code, info, err := compileSWARPairs(fn, ft, emissionMode{production: true})
		if err != nil {
			t.Fatal(err)
		}
		if !ghost {
			baseline, baselineInfo = code, info
		} else if !bytes.Equal(code, baseline) || info.FrameBytes != baselineInfo.FrameBytes || info.SpillSlots != baselineInfo.SpillSlots {
			t.Fatal("dead inner values changed the live outer join", info, baselineInfo)
		}
	}
}

func TestSWARDeadKnownTypesNestedTypingAndCapabilityGates(t *testing.T) {
	ft := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.V128}, Results: []wasm.ValType{wasm.V128}}
	for _, tail := range [][]byte{
		{2, 0x40, 0x6a, 0x1a, 0x0b},             // nested dead block has ordinary stack typing
		{0x43, 0, 0, 0, 0, 0x6a, 0x1a},          // known FP cannot become an unknown integer
		{0x41, 0},                               // known wrong root result in an unreachable frame
		{0x20, 0, 0x41, 0, 0x41, 1, 0x1b, 0x1a}, // known select classes differ
		{0x20, 1, 0x1a},                         // local bounds still checked
		{0xfd, 21, 16, 0x1a},                    // lane immediate still checked
		{0xfd, 0, 5, 0, 0x1a},                   // missing memory/alignment still checked
		{0xfd, 0x94, 2},                         // unknown276
		{0x10, 0}, {0xfb, 0},                    // calls/GC unsupported even when nonexecuted
	} {
		body := append([]byte{0x20, 0, 0x0f}, tail...)
		body = append(body, 0x0b)
		if code, _, err := compileSWARPairs(&wasm.Func{BodyBytes: body}, ft, emissionMode{production: true, interruptible: true}); code != nil || err == nil {
			t.Fatal("invalid/unsupported dead shape emitted", tail, err)
		}
	}
	for _, tail := range [][]byte{
		{0x1b, 0x21, 1}, // unknown select result may satisfy the declared F64 local
		{2, 0x7b, 0x20, 0, 0x0b, 0x1a},
		{0x41, 0, 4, 0x7b, 0x20, 0, 5, 0x20, 0, 0x0b, 0x1a},
		{0x20, 0, 0x41, 0, 0x1b, 0x1a}, // unknown and known V128 select operands
		{3, 0x40, 0x0c, 0, 0x0b},       // nonexecuted loop never gets machine backedges/polls
	} {
		body := append([]byte{0x20, 0, 0x0f}, tail...)
		body = append(body, 0x0b)
		fn, ft := swarValidatedFunction(t, fixtureFunc{params: []byte{0x7b}, result: 0x7b, locals: []byte{1, 1, 0x7c}, body: body})
		if code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true, interruptible: true}); len(code) == 0 || err != nil {
			t.Fatal("valid nonexecuted nested/control/select", tail, err)
		}
	}
	body := append([]byte{0x20, 0, 0x0f}, bytes.Repeat([]byte{0x20, 0}, 257)...)
	body = append(body, bytes.Repeat([]byte{0x1a}, 257)...)
	body = append(body, 0x0b)
	fn, ft := swarValidatedFunction(t, fixtureFunc{params: []byte{0x7b}, result: 0x7b, body: body})
	if code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true}); code != nil || err == nil {
		t.Fatal("unbounded dead virtual operands admitted", err)
	}
}
