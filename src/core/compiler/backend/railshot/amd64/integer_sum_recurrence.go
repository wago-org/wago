//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"

// The recurrence sum uses choose(N,3)=T(N)*(N-2)/3. Since three is odd,
// division by three is multiplication by its inverse modulo the integer width.
// T(N) divides an even factor before multiplication, preserving unsigned extrema.
func (f *fn) emitIntegerGlobalRecurrence(p shared.IntegerSumLoop, typ machineType, get func(uint32)) (bool, error) {
	constant := func(v uint64) { f.pushValue(storage{kind: stConst, typ: typ, cval: int64(v)}) }
	scale := func(v uint64) {
		if v != 1 {
			constant(v)
			f.pushBinOp(opMul, typ)
		}
	}
	// Retain do-while semantics: N=max(1,Bound).
	constant(1)
	get(p.Bound)
	get(p.Bound)
	f.pushUnOp(opEqz, typ)
	f.emitSelect()
	f.setLocal(nil, int(p.Counter), false)
	triangle := func() {
		get(p.Counter)
		constant(1)
		f.pushBinOp(opShrU, typ)
		get(p.Counter)
		constant(1)
		f.pushBinOp(opSub, typ)
		get(p.Counter)
		constant(1)
		f.pushBinOp(opAnd, typ)
		f.pushBinOp(opAdd, typ)
		f.pushBinOp(opMul, typ)
	}
	// Sum C*(g_initial+A*T(i)+B*i)+D*i+E over i=0..N-1.
	// Here Coefficient/Constant include any forwarding through the current store.
	get(p.Accumulator)
	if p.GlobalCoefficient != 0 {
		if err := f.globalGetIndex(p.Global); err != nil {
			return false, err
		}
		get(p.Counter)
		f.pushBinOp(opMul, typ)
		scale(p.GlobalCoefficient)
		f.pushBinOp(opAdd, typ)
	}
	cubic := p.GlobalCoefficient * p.IncrementCoefficient
	linear := p.GlobalCoefficient*p.IncrementConstant + p.Coefficient
	inverseThree := uint64(0xaaaaaaaaaaaaaaab)
	if !p.Wide {
		cubic = uint64(uint32(cubic))
		linear = uint64(uint32(linear))
		inverseThree = 0xaaaaaaab
	}
	if cubic != 0 || linear != 0 {
		triangle()
		if cubic != 0 {
			get(p.Counter)
			constant(2)
			f.pushBinOp(opSub, typ)
			scale(cubic * inverseThree)
			if linear != 0 {
				constant(linear)
				f.pushBinOp(opAdd, typ)
			}
			f.pushBinOp(opMul, typ)
		} else {
			scale(linear)
		}
		f.pushBinOp(opAdd, typ)
	}
	if p.Constant != 0 {
		get(p.Counter)
		scale(p.Constant)
		f.pushBinOp(opAdd, typ)
	}
	f.setLocal(nil, int(p.Accumulator), false)
	// Publish g_final=g_initial+A*T(N)+B*N through the canonical setter.
	if err := f.globalGetIndex(p.Global); err != nil {
		return false, err
	}
	if p.IncrementCoefficient != 0 {
		triangle()
		scale(p.IncrementCoefficient)
		f.pushBinOp(opAdd, typ)
	}
	if p.IncrementConstant != 0 {
		get(p.Counter)
		scale(p.IncrementConstant)
		f.pushBinOp(opAdd, typ)
	}
	if err := f.globalSetIndex(p.Global); err != nil {
		return false, err
	}
	for _, x := range []uint32{p.Counter, p.Accumulator} {
		if x < 64 {
			f.localWritten |= uint64(1) << x
		}
	}
	f.stats.peep("integer-sum-global-recurrence")
	f.stats.peep("integer-sum-loop")
	return true, nil
}
