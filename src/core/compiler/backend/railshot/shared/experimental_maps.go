package shared

import "github.com/wago-org/wago/src/core/compiler/wasm"

// MapPlan separates i32 address recurrences from the loaded scalar data. This
// narrow first prototype supports one input, one output and two ordered binary
// operations. Its four lanes always use 128 bits on both target emitters.
type MapPlan struct {
	Loop                                                         ReplicationPlan
	Destination, Source, Scale, Bias, Temporary, VectorTemporary uint32
	Element                                                      wasm.ValType
	ElementBytes, Lanes                                          uint32
	FirstOperation, SecondOperation                              uint32
	LoadOffset, StoreOffset                                      uint64
}

func mapOpcode(op byte, fp bool) uint32 {
	if fp {
		switch op {
		case 0x92:
			return 228
		case 0x93:
			return 229
		case 0x94:
			return 230
		}
	}
	switch op {
	case 0x6a:
		return 174
	case 0x6b:
		return 177
	case 0x6c:
		return 181
	case 0x71:
		return 78
	case 0x72:
		return 80
	case 0x73:
		return 81
	}
	return 0
}
func InspectMap(m *wasm.Module, function int, fp bool, p *MapPlan) string {
	*p = MapPlan{ElementBytes: 4, Lanes: 4}
	body := m.Code[function].BodyBytes
	if why := InspectReplication(body, m, 4, false, false, &p.Loop); why != "" {
		return why
	}
	ops := p.Loop.Operations[:p.Loop.OperationN]
	if len(ops) != 17 {
		return "map-operation-count"
	}
	load, store := byte(0x28), byte(0x36)
	p.Element = wasm.I32
	if fp {
		load, store, p.Element = 0x2a, 0x38, wasm.F32
	}
	want := [17]byte{0x20, 0x20, load, 0x20, 0, 0x20, 0, 0x22, store, 0x20, 0x41, 0x6a, 0x21, 0x20, 0x41, 0x6a, 0x21}
	for i, x := range ops {
		if want[i] != 0 && x.Opcode != want[i] {
			return "map-shape"
		}
	}
	p.FirstOperation, p.SecondOperation = mapOpcode(ops[4].Opcode, fp), mapOpcode(ops[6].Opcode, fp)
	if p.FirstOperation == 0 || p.SecondOperation == 0 {
		return "map-arithmetic"
	}
	p.Destination, p.Source, p.Scale, p.Bias, p.Temporary = ops[0].Local, ops[1].Local, ops[3].Local, ops[5].Local, ops[7].Local
	if ops[9].Local != p.Destination || ops[12].Local != p.Destination || ops[13].Local != p.Source || ops[16].Local != p.Source {
		return "map-address-update"
	}
	for _, i := range []int{10, 14} {
		if !mapConstant(body, &p.Loop, ops[i], 4) {
			return "map-step"
		}
	}
	p.LoadOffset, p.StoreOffset = ops[2].Offset, ops[8].Offset
	if p.LoadOffset != 0 || p.StoreOffset != 0 {
		return "map-immediate-offset"
	}
	ft, ok := m.LocalFuncType(function)
	if !ok {
		return "map-type"
	}
	var types [ExperimentMaxLocals]wasm.ValType
	n := len(ft.Params)
	if n > len(types) {
		return "map-locals"
	}
	copy(types[:], ft.Params)
	for _, run := range m.Code[function].Locals.Runs {
		if run.Count > uint32(len(types)-n) {
			return "map-locals"
		}
		for j := uint32(0); j < run.Count; j++ {
			types[n] = run.Type
			n++
		}
	}
	if n == len(types) {
		return "map-vector-local"
	}
	p.VectorTemporary = uint32(n)
	// No data local aliases a pointer, counter or another written value. Input
	// parameters are invariant; only addresses, count and scalar output change.
	if p.Source == p.Destination || p.Scale == p.Bias {
		return "map-local-alias"
	}
	for _, a := range []uint32{p.Destination, p.Source, p.Loop.Counter} {
		if a >= uint32(n) || types[a] != wasm.I32 {
			return "map-address-type"
		}
	}
	for _, a := range []uint32{p.Scale, p.Bias, p.Temporary} {
		if a >= uint32(n) || types[a] != p.Element {
			return "map-data-type"
		}
	}
	var seen uint16
	for _, a := range []uint32{p.Destination, p.Source, p.Loop.Counter, p.Scale, p.Bias, p.Temporary} {
		if seen&(1<<a) != 0 {
			return "map-local-alias"
		}
		seen |= 1 << a
	}
	return ""
}
func mapConstant(body []byte, p *ReplicationPlan, x ExperimentOperation, value int32) bool {
	r := wasm.ReaderFrom(body)
	if r.JumpTo(p.BodyStart+int(x.Start)+1) != nil {
		return false
	}
	v, err := r.I32()
	return err == nil && v == value
}
func mapSIMD(out []byte, op uint32) []byte      { return experimentU32(append(out, 0xfd), op) }
func mapExtended(out []byte, idx uint32) []byte { return append(experimentGet(out, idx), 0xad) }
func (p *MapPlan) endAddress(out []byte, idx uint32) []byte {
	out = mapExtended(out, idx)
	out = mapExtended(out, p.Loop.Counter)
	return append(out, 0x42, 4, 0x7e, 0x7c)
}
func mapLimit(out []byte) []byte { return append(out, 0x42, 0x80, 0x80, 0x80, 0x80, 0x10) } // i64.const 2^32
func (p *MapPlan) guard(out []byte) []byte {
	// uint32 address + uint32 count*4 fits uint64. The i32 recurrence and the
	// memory immediate stay separate. No memory access occurs in these guards.
	for i, idx := range []uint32{p.Source, p.Destination} {
		out = p.endAddress(out, idx)
		out = mapLimit(out)
		out = append(out, 0x58) // i64.le_u
		out = p.endAddress(out, idx)
		out = append(out, 0x3f, 0, 0xad, 0x42, 16, 0x86, 0x58, 0x71) // <= memory.size*65536
		if i != 0 {
			out = append(out, 0x71)
		}
	}
	// Permit exact overlap and disjoint full ranges. Partial overlap selects
	// the original scalar loop, preserving effects before later loads or traps.
	out = experimentGet(out, p.Source)
	out = experimentGet(out, p.Destination)
	out = append(out, 0x46)
	out = p.endAddress(out, p.Source)
	out = mapExtended(out, p.Destination)
	out = append(out, 0x58, 0x72)
	out = p.endAddress(out, p.Destination)
	out = mapExtended(out, p.Source)
	out = append(out, 0x58, 0x72, 0x71)
	return out
}
func (p *MapPlan) vectorBody(out []byte) []byte {
	out = experimentGet(out, p.Destination)
	out = experimentGet(out, p.Source)
	out = mapSIMD(out, 0)
	out = append(out, 0, 0)
	splat, extract := uint32(17), uint32(27)
	if p.Element == wasm.F32 {
		splat, extract = 19, 31
	}
	out = experimentGet(out, p.Scale)
	out = mapSIMD(out, splat)
	out = mapSIMD(out, p.FirstOperation)
	out = experimentGet(out, p.Bias)
	out = mapSIMD(out, splat)
	out = mapSIMD(out, p.SecondOperation)
	out = experimentU32(append(out, 0x22), p.VectorTemporary)
	out = mapSIMD(out, 11)
	out = append(out, 0, 0)
	out = experimentGet(out, p.VectorTemporary)
	out = mapSIMD(out, extract)
	out = append(out, 3)
	out = experimentSet(out, p.Temporary)
	for _, idx := range []uint32{p.Destination, p.Source} {
		out = experimentGet(out, idx)
		out = append(out, 0x41, 16, 0x6a)
		out = experimentSet(out, idx)
	}
	out = experimentGet(out, p.Loop.Counter)
	out = append(out, 0x41, 4, 0x6b)
	return experimentSet(out, p.Loop.Counter)
}
func (p *MapPlan) Emit(body []byte, assertFast bool) []byte {
	out := make([]byte, 0, len(body)+1024)
	out = append(out, body[:p.Loop.Start]...)
	out = p.guard(out)
	out = append(out, 0x04, 0x40)
	out = append(out, 0x02, 0x40, 0x02, 0x40, 0x03, 0x40)
	out = experimentGet(out, p.Loop.Counter)
	out = append(out, 0x41, 4, 0x49, 0x0d, 1)
	out = p.vectorBody(out)
	out = append(out, 0x0c, 0, 0x0b, 0x0b, 0x03, 0x40)
	out = p.Loop.zeroExit(out)
	out = p.Loop.replay(out, body)
	out = append(out, 0x0c, 0, 0x0b, 0x0b, 0x05)
	if assertFast {
		out = append(out, 0x00)
	}
	out = append(out, body[p.Loop.Start:p.Loop.End]...)
	out = append(out, 0x0b)
	return append(out, body[p.Loop.End:]...)
}
func RewriteVectorMaps(m *wasm.Module, fp, assertFast bool) (*wasm.Module, string, error) {
	if len(m.Tags) != 0 || len(m.Customs) != 0 {
		return m, "tags-or-custom", nil
	}
	mt, ok := m.MemoryType(0)
	if !ok || mt.Shared || mt.Limits.Addr64 {
		return m, "memory-kind", nil
	}
	var out *wasm.Module
	growth := 0
	reason := "no-map"
	for i, f := range m.Code {
		var p MapPlan
		if why := InspectMap(m, i, fp, &p); why != "" {
			reason = why
			continue
		}
		replacement := p.Emit(f.BodyBytes, assertFast)
		delta := len(replacement) - len(f.BodyBytes)
		if delta > ExperimentMaxFunctionGrowth || growth+delta > ExperimentMaxModuleGrowth {
			reason = "growth"
			continue
		}
		if out == nil {
			copy := *m
			out = &copy
			out.Code = append([]wasm.Func(nil), m.Code...)
			out.BranchHints = nil
			out.ExperimentalInstructionOrigins = make([][]uint32, len(m.Code))
		}
		out.Code[i].BodyBytes = replacement
		out.ExperimentalInstructionOrigins[i] = experimentOrigins(m, f.BodyBytes, replacement, &p.Loop, true)
		out.Code[i].Body = wasm.Expr{}
		out.Code[i].Locals.Runs = append(append([]wasm.LocalRun(nil), f.Locals.Runs...), wasm.LocalRun{Count: 1, Type: wasm.V128})
		growth += delta
	}
	if out == nil {
		return m, reason, nil
	}
	if err := wasm.ValidateModule(out); err != nil {
		return m, "validation", err
	}
	return out, "accepted", nil
}
