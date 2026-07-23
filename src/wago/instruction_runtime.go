package wago

import "fmt"

const instructionABIModule = "wago:abi"

type instructionHostFunc HostFunc
type instructionTrap struct{ err error }

type instructionResultPack struct{ values []Bits }

type instructionState struct {
	next   uint32
	values map[uint32]Bits
	packs  map[uint32]instructionResultPack
}

func instructionCallingInstance(m HostModule) (*Instance, bool) {
	switch h := m.(type) {
	case instanceHostModule:
		return h.in, h.in != nil
	case staticHostModule:
		return h.in, h.in != nil
	default:
		return nil, false
	}
}

func (s *instructionState) allocValue(v Bits) uint32 {
	if s.values == nil {
		s.values = make(map[uint32]Bits)
	}
	id := s.allocID()
	s.values[id] = v.Clone()
	return id
}

func (s *instructionState) allocPack(values []Bits) uint32 {
	if s.packs == nil {
		s.packs = make(map[uint32]instructionResultPack)
	}
	id := s.allocID()
	copyValues := make([]Bits, len(values))
	for i := range values {
		copyValues[i] = values[i].Clone()
	}
	s.packs[id] = instructionResultPack{values: copyValues}
	return id
}

func (s *instructionState) allocID() uint32 {
	// Never recycle IDs: a stale i32 can therefore never become a valid handle
	// again. Exhaustion is fantastically remote but deterministic and safe.
	if s.next == ^uint32(0) {
		panic(instructionTrap{fmt.Errorf("wago: custom instruction handle space exhausted")})
	}
	s.next++
	return s.next
}

func customCarrierType(typ CustomType) ValType {
	switch typ.Carrier() {
	case WasmI64:
		return ValI64
	case WasmF32:
		return ValF32
	case WasmF64:
		return ValF64
	case WasmV128:
		return ValV128
	case WasmFuncRef:
		return ValFuncRef
	case WasmExternRef:
		return ValExternRef
	default:
		return ValI32
	}
}

func instructionImport(ins *registeredInstruction) *registeredImport {
	params := make([]ValType, len(ins.spec.Input))
	for i := range params {
		params[i] = ValI32
		if ins.spec.Custom != nil && !ins.spec.Custom.Inputs[i].IsZero() {
			params[i] = customCarrierType(ins.spec.Custom.Inputs[i])
		}
	}
	var results []ValType
	if len(ins.spec.Output) > 0 {
		results = []ValType{ValI32}
		if ins.spec.Custom != nil && ins.spec.Custom.Output != nil {
			results[0] = customCarrierType(*ins.spec.Custom.Output)
		}
	}
	fn := instructionHostFunc(func(m HostModule, raw, result []uint64) {
		if ins.spec.Custom != nil {
			panic(instructionTrap{fmt.Errorf("wago: custom instruction %s.%s requires native lowering", ins.spec.Module, ins.spec.Name)})
		}
		in, ok := instructionCallingInstance(m)
		if !ok {
			panic(instructionTrap{fmt.Errorf("wago: instruction called without an instance")})
		}
		args := make([]Bits, len(ins.spec.Input))
		for i, width := range ins.spec.Input {
			word := uint32(raw[i])
			if width <= 32 {
				v, err := BitsFromUint32(width, word)
				if err != nil {
					panic(instructionTrap{err})
				}
				args[i] = v
				continue
			}
			v, ok := in.instructionState.values[word]
			if !ok || !v.ValidFor(width) {
				panic(instructionTrap{fmt.Errorf("wago: instruction %s.%s input %d: invalid %d-bit value handle %d", ins.spec.Module, ins.spec.Name, i, width, word)})
			}
			args[i] = v.Clone()
		}
		out, err := ins.spec.Handler(m, args)
		if err != nil {
			panic(instructionTrap{fmt.Errorf("wago: instruction %s.%s: %w", ins.spec.Module, ins.spec.Name, err)})
		}
		if len(out) != len(ins.spec.Output) {
			panic(instructionTrap{fmt.Errorf("wago: instruction %s.%s returned %d value(s), want %d", ins.spec.Module, ins.spec.Name, len(out), len(ins.spec.Output))})
		}
		for i, width := range ins.spec.Output {
			if !out[i].ValidFor(width) {
				panic(instructionTrap{fmt.Errorf("wago: instruction %s.%s output %d has width %d, want %d", ins.spec.Module, ins.spec.Name, i, out[i].Width(), width)})
			}
		}
		switch len(out) {
		case 0:
		case 1:
			if out[0].Width() <= 32 {
				result[0] = uint64(out[0].Uint32())
			} else {
				result[0] = uint64(in.instructionState.allocValue(out[0]))
			}
		default:
			result[0] = uint64(in.instructionState.allocPack(out))
		}
	})
	return &registeredImport{module: ins.spec.Module, name: ins.spec.Name, fn: HostFunc(fn), params: params, results: results, docs: "custom instruction"}
}

func instructionABIImports() []*registeredImport {
	get := instructionHostFunc(func(m HostModule, p, r []uint64) {
		in, ok := instructionCallingInstance(m)
		if !ok {
			panic(instructionTrap{fmt.Errorf("wago: result.get called without an instance")})
		}
		id, index := uint32(p[0]), uint32(p[1])
		pack, ok := in.instructionState.packs[id]
		if !ok || int(index) >= len(pack.values) {
			panic(instructionTrap{fmt.Errorf("wago: invalid result pack %d index %d", id, index)})
		}
		v := pack.values[index]
		if v.Width() <= 32 {
			r[0] = uint64(v.Uint32())
		} else {
			r[0] = uint64(in.instructionState.allocValue(v))
		}
	})
	dropResult := instructionHostFunc(func(m HostModule, p, _ []uint64) {
		in, ok := instructionCallingInstance(m)
		if !ok {
			panic(instructionTrap{fmt.Errorf("wago: result.drop called without an instance")})
		}
		id := uint32(p[0])
		if _, ok := in.instructionState.packs[id]; !ok {
			panic(instructionTrap{fmt.Errorf("wago: invalid result pack handle %d", id)})
		}
		delete(in.instructionState.packs, id)
	})
	dropValue := instructionHostFunc(func(m HostModule, p, _ []uint64) {
		in, ok := instructionCallingInstance(m)
		if !ok {
			panic(instructionTrap{fmt.Errorf("wago: value.drop called without an instance")})
		}
		id := uint32(p[0])
		if _, ok := in.instructionState.values[id]; !ok {
			panic(instructionTrap{fmt.Errorf("wago: invalid value handle %d", id)})
		}
		delete(in.instructionState.values, id)
	})
	return []*registeredImport{
		{module: instructionABIModule, name: "result.get", fn: HostFunc(get), params: []ValType{ValI32, ValI32}, results: []ValType{ValI32}, docs: "project a custom-instruction result pack"},
		{module: instructionABIModule, name: "result.drop", fn: HostFunc(dropResult), params: []ValType{ValI32}, docs: "release a custom-instruction result pack"},
		{module: instructionABIModule, name: "value.drop", fn: HostFunc(dropValue), params: []ValType{ValI32}, docs: "release a wide custom-instruction value"},
	}
}

func validateInstructionSignature(key string, spec InstructionSpec, sig FuncSig) error {
	params := make([]ValType, len(spec.Input))
	for i := range params {
		params[i] = ValI32
		if spec.Custom != nil && !spec.Custom.Inputs[i].IsZero() {
			params[i] = customCarrierType(spec.Custom.Inputs[i])
		}
	}
	wantResults := 0
	if len(spec.Output) > 0 {
		wantResults = 1
	}
	results := make([]ValType, wantResults)
	if wantResults != 0 {
		results[0] = ValI32
		if spec.Custom != nil && spec.Custom.Output != nil {
			results[0] = customCarrierType(*spec.Custom.Output)
		}
	}
	return validatePhysicalImportSignature(key, params, results, sig)
}

func validatePhysicalImportSignature(key string, params, results []ValType, sig FuncSig) error {
	if len(sig.Params) != len(params) || len(sig.Results) != len(results) {
		return fmt.Errorf("compile: instruction ABI import %q has signature %v -> %v, want %v -> %v", key, sig.Params, sig.Results, params, results)
	}
	for i := range params {
		if sig.Params[i] != params[i] {
			return fmt.Errorf("compile: instruction ABI import %q parameter %d is %v, want %v", key, i, sig.Params[i], params[i])
		}
	}
	for i := range results {
		if sig.Results[i] != results[i] {
			return fmt.Errorf("compile: instruction ABI import %q result %d is %v, want %v", key, i, sig.Results[i], results[i])
		}
	}
	return nil
}
