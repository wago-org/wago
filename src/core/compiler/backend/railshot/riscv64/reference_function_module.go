//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"slices"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func admitPublicDynamicHostReferenceBoundaryWire(data []byte) bool {
	if !admitExactProductWireShape(data, nil) {
		return false
	}
	decoded, err := wasm.DecodeModuleByteBackedWithLimits(data, wasm.ValidationFeatures{}, wasm.DecodeLimits{MaxTypes: MaxFunctions, MaxMetadataBytes: 16 << 20})
	return err == nil && admitPublicDynamicHostReferenceBoundary(decoded.Module)
}

func admitPublicReferenceCallBoundaryWire(data []byte) bool {
	if !publicReferenceCallBoundaryWireShape(data) {
		return false
	}
	decoded, err := wasm.DecodeModuleByteBackedWithLimits(data, wasm.ValidationFeatures{}, wasm.DecodeLimits{MaxTypes: MaxFunctions, MaxMetadataBytes: 16 << 20})
	return err == nil && admitPublicReferenceCallBoundary(decoded.Module)
}

func publicReferenceCallBoundaryWireU32(data []byte, offset *int) (uint32, bool) {
	start := *offset
	var value uint32
	for shift := uint(0); shift < 35 && *offset < len(data); shift += 7 {
		b := data[*offset]
		(*offset)++
		if shift == 28 && b > 0x0f {
			return 0, false
		}
		value |= uint32(b&0x7f) << shift
		if b&0x80 == 0 {
			canonicalBytes := 1
			for rest := value; rest >= 0x80; rest >>= 7 {
				canonicalBytes++
			}
			return value, *offset-start == canonicalBytes
		}
	}
	return 0, false
}

func publicReferenceCallBoundaryWireType(data []byte, offset *int) (params, results []byte, ok bool) {
	if *offset >= len(data) || data[*offset] != 0x60 {
		return nil, nil, false
	}
	(*offset)++
	readTypes := func() ([]byte, bool) {
		n, valid := publicReferenceCallBoundaryWireU32(data, offset)
		if !valid || n > 256 || uint64(*offset)+uint64(n) > uint64(len(data)) {
			return nil, false
		}
		out := data[*offset : *offset+int(n)]
		for _, typ := range out {
			switch typ {
			case 0x7f, 0x7e, 0x7d, 0x7c, 0x70, 0x6f:
			default:
				return nil, false
			}
		}
		*offset += int(n)
		return out, true
	}
	params, ok = readTypes()
	if !ok {
		return nil, nil, false
	}
	results, ok = readTypes()
	return
}

// The semantic decoder intentionally runs only after this bounded canonical
// candidate filter. Besides avoiding a second decode of ordinary
// production modules, it retains wire provenance that the decoded Module
// cannot represent: compact function types, canonical section/count/body
// lengths, exact fixed directories, and the canonical public wrapper.
func publicReferenceCallBoundaryWireShape(data []byte) bool {
	if len(data) < 8 || !bytes.Equal(data[:8], []byte{0, 'a', 's', 'm', 1, 0, 0, 0}) {
		return false
	}
	offset := 8
	var sections [5][]byte
	for i, id := range []byte{1, 3, 7, 9, 10} {
		if offset >= len(data) || data[offset] != id {
			return false
		}
		offset++
		n, ok := publicReferenceCallBoundaryWireU32(data, &offset)
		if !ok || uint64(offset)+uint64(n) > uint64(len(data)) {
			return false
		}
		sections[i] = data[offset : offset+int(n)]
		offset += int(n)
	}
	if offset != len(data) || !bytes.Equal(sections[1], []byte{3, 0, 1, 2}) || !bytes.Equal(sections[2], []byte{3, 6, 't', 'a', 'r', 'g', 'e', 't', 0, 0, 3, 'g', 'e', 't', 0, 1, 4, 'c', 'a', 'l', 'l', 0, 2}) || !bytes.Equal(sections[3], []byte{1, 3, 0, 1, 0}) {
		return false
	}

	typeOffset := 0
	count, ok := publicReferenceCallBoundaryWireU32(sections[0], &typeOffset)
	if !ok || count != 3 {
		return false
	}
	targetParams, targetResults, ok := publicReferenceCallBoundaryWireType(sections[0], &typeOffset)
	if !ok {
		return false
	}
	producerParams, producerResults, ok := publicReferenceCallBoundaryWireType(sections[0], &typeOffset)
	if !ok || len(producerParams) != 0 || !bytes.Equal(producerResults, []byte{0x70}) {
		return false
	}
	if typeOffset >= len(sections[0]) || sections[0][typeOffset] != 0x60 {
		return false
	}
	typeOffset++
	callerParamCount, ok := publicReferenceCallBoundaryWireU32(sections[0], &typeOffset)
	if !ok || callerParamCount != uint32(len(targetParams)+1) || typeOffset+len(targetParams)+2 > len(sections[0]) || !bytes.Equal(sections[0][typeOffset:typeOffset+len(targetParams)], targetParams) {
		return false
	}
	typeOffset += len(targetParams)
	if !bytes.Equal(sections[0][typeOffset:typeOffset+2], []byte{0x63, 0}) {
		return false
	}
	typeOffset += 2
	callerResultCount, ok := publicReferenceCallBoundaryWireU32(sections[0], &typeOffset)
	if !ok || uint64(typeOffset)+uint64(callerResultCount) > uint64(len(sections[0])) {
		return false
	}
	callerResults := sections[0][typeOffset : typeOffset+int(callerResultCount)]
	typeOffset += int(callerResultCount)
	sameResults := bytes.Equal(callerResults, targetResults)
	referenceResult := bytes.Equal(callerResults, []byte{0x70})
	referenceCarriers := false
	for _, types := range [][]byte{targetParams, targetResults} {
		for _, typ := range types {
			referenceCarriers = referenceCarriers || typ == 0x70 || typ == 0x6f
		}
	}
	if typeOffset != len(sections[0]) || !sameResults && !referenceResult || referenceCarriers && !sameResults {
		return false
	}

	codeOffset := 0
	codeCount, ok := publicReferenceCallBoundaryWireU32(sections[4], &codeOffset)
	if !ok || codeCount != 3 {
		return false
	}
	var bodies [3][]byte
	for i := range bodies {
		n, valid := publicReferenceCallBoundaryWireU32(sections[4], &codeOffset)
		if !valid || n == 0 || uint64(codeOffset)+uint64(n) > uint64(len(sections[4])) {
			return false
		}
		bodies[i] = sections[4][codeOffset : codeOffset+int(n)]
		codeOffset += int(n)
		if bodies[i][0] != 0 { // canonical zero local-declaration vector
			return false
		}
	}
	if codeOffset != len(sections[4]) || !bytes.Equal(bodies[1], []byte{0, 0xd2, 0, 0x0b}) {
		return false
	}
	wrapper := []byte{0}
	for i := range targetParams {
		wrapper = append(wrapper, 0x20)
		wrapper = appendPublicReferenceBoundaryU32(wrapper, uint32(i))
	}
	wrapper = append(wrapper, 0x20)
	wrapper = appendPublicReferenceBoundaryU32(wrapper, uint32(len(targetParams)))
	wrapper = append(wrapper, 0x14, 0)
	if referenceResult && !sameResults {
		for range targetResults {
			wrapper = append(wrapper, 0x1a)
		}
		wrapper = append(wrapper, 0x20)
		wrapper = appendPublicReferenceBoundaryU32(wrapper, uint32(len(targetParams)))
	}
	wrapper = append(wrapper, 0x0b)
	if bytes.Equal(bodies[2], wrapper) {
		return true
	}
	if !sameResults || len(wrapper) < 3 {
		return false
	}
	wrapper[len(wrapper)-3] = 0x15 // canonical return_call_ref alternate
	return bytes.Equal(bodies[2], wrapper)
}

// admitPublicReferenceCallBoundary is the first public owner-scoped call_ref
// surface. It is deliberately table-free and reference-global-free: a target,
// a token-producing wrapper, and a wrapper that forwards the target parameters
// plus one nullable exact target reference to call_ref or return_call_ref. The
// bounded target vector may mix shared scalars with nullable abstract funcref
// and externref carriers; a proper tail is admitted only when the wrapper
// returns the target's exact result vector. The compiler witness, public token
// authentication, retained tail plan, and invocation owner graph are all
// required; no other public dynamic-reference shape inherits this exception.
func admitPublicReferenceCallBoundary(m *wasm.Module) bool {
	if m == nil || len(m.Imports) != 0 || len(m.Types) != 3 || len(m.FuncTypes) != 3 || len(m.Code) != 3 || len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Data) != 0 || m.DataCount != nil || len(m.Elements) != 1 || m.Start != nil || len(m.Exports) != 3 || len(m.Customs) != 0 || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	for i := range m.Types {
		if len(m.Types[i].SubTypes) != 1 {
			return false
		}
		sub := m.Types[i].SubTypes[0]
		if !sub.Final || sub.HasPrefix || len(sub.Supers) != 0 || sub.Metadata.Describes.Present() || sub.Metadata.Descriptor.Present() || sub.Comp.Kind != wasm.CompFunc || len(sub.Comp.Fields) != 0 || sub.Comp.Array != (wasm.FieldType{}) {
			return false
		}
	}
	target := m.Types[0].SubTypes[0].Comp
	producer := m.Types[1].SubTypes[0].Comp
	caller := m.Types[2].SubTypes[0].Comp
	indexedTarget := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{}), false))
	referenceCarriers := publicReferenceCarrierTarget(&target)
	if integerNumericSignature(&target, true) != nil && !referenceCarriers || len(producer.Params) != 0 || len(producer.Results) != 1 || !wasm.EqualValType(producer.Results[0], wasm.FuncRef) || len(caller.Params) != len(target.Params)+1 || !wasm.EqualValType(caller.Params[len(target.Params)], indexedTarget) {
		return false
	}
	for i := range target.Params {
		if !wasm.EqualValType(caller.Params[i], target.Params[i]) {
			return false
		}
	}
	numericResult := len(caller.Results) == len(target.Results)
	for i := 0; numericResult && i < len(target.Results); i++ {
		numericResult = wasm.EqualValType(caller.Results[i], target.Results[i])
	}
	referenceResult := len(caller.Results) == 1 && wasm.EqualValType(caller.Results[0], wasm.FuncRef)
	if !numericResult && !referenceResult || referenceCarriers && !numericResult {
		return false
	}
	for i := range m.FuncTypes {
		if m.FuncTypes[i].Rec || m.FuncTypes[i].Index != uint32(i) || len(m.Code[i].Locals.Runs) != 0 || len(m.Code[i].Body.Instrs) != 0 {
			return false
		}
	}
	e := m.Elements[0]
	if e.Mode.Kind != wasm.ElemDeclarative || e.Mode.Table != 0 || len(e.Mode.Offset.BodyBytes) != 0 || len(e.Mode.Offset.Instrs) != 0 || e.Kind.Kind != wasm.ElemFuncs || e.Kind.Ref != (wasm.RefType{}) || len(e.Kind.Funcs) != 1 || e.Kind.Funcs[0] != 0 || len(e.Kind.Exprs) != 0 {
		return false
	}
	callerBody := make([]byte, 0, 3*(len(target.Params)+len(target.Results))+4)
	for i := range target.Params {
		callerBody = append(callerBody, 0x20)
		callerBody = appendPublicReferenceBoundaryU32(callerBody, uint32(i))
	}
	callerBody = append(callerBody, 0x20)
	callerBody = appendPublicReferenceBoundaryU32(callerBody, uint32(len(target.Params)))
	callerPrefix := slices.Clone(callerBody)
	callerBody = append(callerBody, 0x14, 0)
	if referenceResult {
		for range target.Results {
			callerBody = append(callerBody, 0x1a)
		}
		callerBody = append(callerBody, 0x20)
		callerBody = appendPublicReferenceBoundaryU32(callerBody, uint32(len(target.Params)))
	}
	callerBody = append(callerBody, 0x0b)
	tailBody := append(callerPrefix, 0x15, 0, 0x0b)
	if !bytes.Equal(m.Code[1].BodyBytes, []byte{0xd2, 0, 0x0b}) || !bytes.Equal(m.Code[2].BodyBytes, callerBody) && (!numericResult || !bytes.Equal(m.Code[2].BodyBytes, tailBody)) {
		return false
	}
	for i, name := range []string{"target", "get", "call"} {
		export := m.Exports[i]
		if export.Name != name || export.Index.Kind != wasm.ExternFunc || export.Index.Index != uint32(i) {
			return false
		}
	}
	return true
}

// admitPublicDynamicHostReferenceBoundary pins the first reference-bearing
// public call_ref/return_call_ref bridge to one exact imported host target, one local
// identity and one selector wrapper. The bounded structural wire gate retains
// the canonical representation contract; this decoded check independently
// binds every semantic directory and type before the exception reaches lowering.
func admitPublicDynamicHostReferenceBoundary(m *wasm.Module) bool {
	if m == nil || len(m.Imports) != 1 || m.Imports[0].Module != "env" || m.Imports[0].Name != "target" || m.Imports[0].Type != wasm.NewFuncExternType(wasm.TypeIdx{}) || len(m.Types) != 4 || len(m.FuncTypes) != 2 || len(m.Code) != 2 || len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Data) != 0 || m.DataCount != nil || len(m.Elements) != 1 || m.Start != nil || len(m.Exports) != 2 || len(m.Customs) != 0 || m.HasNameMetadata() || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	for i := range m.Types {
		if len(m.Types[i].SubTypes) != 1 {
			return false
		}
		sub := m.Types[i].SubTypes[0]
		if !sub.Final || sub.HasPrefix || len(sub.Supers) != 0 || sub.Metadata.Describes.Present() || sub.Metadata.Descriptor.Present() || sub.Comp.Kind != wasm.CompFunc || len(sub.Comp.Fields) != 0 || sub.Comp.Array != (wasm.FieldType{}) {
			return false
		}
	}
	want := []wasm.ValType{wasm.I32, wasm.FuncRef, wasm.ExternRef, wasm.I64, wasm.F32, wasm.F64}
	target := &m.Types[0].SubTypes[0].Comp
	if !publicReferenceCarrierTarget(target) || !slices.EqualFunc(target.Params, want, wasm.EqualValType) || !slices.EqualFunc(target.Results, want, wasm.EqualValType) {
		return false
	}
	for _, index := range []int{2} {
		typ := &m.Types[index].SubTypes[0].Comp
		if !slices.EqualFunc(typ.Params, want, wasm.EqualValType) || !slices.EqualFunc(typ.Results, want, wasm.EqualValType) {
			return false
		}
	}
	caller := &m.Types[1].SubTypes[0].Comp
	producer := &m.Types[3].SubTypes[0].Comp
	if len(caller.Params) != len(want)+1 || !slices.EqualFunc(caller.Params[:len(want)], want, wasm.EqualValType) || !wasm.EqualValType(caller.Params[len(want)], wasm.I32) || !slices.EqualFunc(caller.Results, want, wasm.EqualValType) || len(producer.Params) != 0 || !slices.EqualFunc(producer.Results, want, wasm.EqualValType) {
		return false
	}
	if m.FuncTypes[0] != (wasm.TypeIdx{Index: 0}) || m.FuncTypes[1] != (wasm.TypeIdx{Index: 1}) || m.Code[0].LocalDeclBytes != 1 || m.Code[1].LocalDeclBytes != 1 || len(m.Code[0].Locals.Runs) != 0 || len(m.Code[1].Locals.Runs) != 0 || len(m.Code[0].Body.Instrs) != 0 || len(m.Code[1].Body.Instrs) != 0 || len(m.Code[0].Body.BodyBytes) != 0 || len(m.Code[1].Body.BodyBytes) != 0 {
		return false
	}
	identity := []byte{0x20, 0, 0x20, 1, 0x20, 2, 0x20, 3, 0x20, 4, 0x20, 5, 0x0b}
	tailBody := []byte{0x20, 6, 0x04, 3, 0x20, 0, 0x20, 1, 0x20, 2, 0x20, 3, 0x20, 4, 0x20, 5, 0xd0, 2, 0x15, 2, 0x05, 0x20, 0, 0x20, 1, 0x20, 2, 0x20, 3, 0x20, 4, 0x20, 5, 0xd2, 0, 0x15, 2, 0x0b, 0x0b}
	callBody := slices.Clone(tailBody)
	callBody[18], callBody[35] = 0x14, 0x14
	if !bytes.Equal(m.Code[0].BodyBytes, identity) || !bytes.Equal(m.Code[1].BodyBytes, tailBody) && !bytes.Equal(m.Code[1].BodyBytes, callBody) {
		return false
	}
	e := m.Elements[0]
	if e.Mode.Kind != wasm.ElemDeclarative || e.Mode.Table != 0 || len(e.Mode.Offset.BodyBytes) != 0 || len(e.Mode.Offset.Instrs) != 0 || e.Kind.Kind != wasm.ElemFuncs || e.Kind.Ref != (wasm.RefType{}) || len(e.Kind.Funcs) != 1 || e.Kind.Funcs[0] != 0 || len(e.Kind.Exprs) != 0 {
		return false
	}
	return m.Exports[0].Name == "call" && m.Exports[0].Index == (wasm.ExternIdx{Kind: wasm.ExternFunc, Index: 2}) && m.Exports[1].Name == "identity" && m.Exports[1].Index == (wasm.ExternIdx{Kind: wasm.ExternFunc, Index: 1})
}

func publicReferenceCarrierTarget(target *wasm.CompType) bool {
	if target == nil || target.Kind != wasm.CompFunc || len(target.Params) > 256 || len(target.Results) > 256 {
		return false
	}
	reference := false
	for _, values := range [][]wasm.ValType{target.Params, target.Results} {
		for _, typ := range values {
			if scalarType(typ, true) {
				continue
			}
			if !wasm.EqualValType(typ, wasm.FuncRef) && !wasm.EqualValType(typ, wasm.ExternRef) {
				return false
			}
			reference = true
		}
	}
	return reference
}

func appendPublicReferenceBoundaryU32(dst []byte, value uint32) []byte {
	for value >= 0x80 {
		dst = append(dst, byte(value)|0x80)
		value >>= 7
	}
	return append(dst, byte(value))
}

// This private decoded-module prerequisite proves that reference-bearing
// functions cannot escape through a descriptor, import or resource. A direct
// function export is allowed only through the public wrapper boundary, which
// authenticates opaque tokens and never makes that function a ref.func target.
// It does not authorize production staging or general reference ownership.
// Every bound precedes frontend validation and derived carrier allocation.
func admitClosedReferenceFunctionModule(m *wasm.Module, vector bool) error {
	if m == nil || len(m.Imports) > MaxProductionImports || len(m.Code) == 0 || len(m.Code) > MaxFunctions || len(m.FuncTypes) != len(m.Code) || len(m.Types) > MaxFunctions || len(m.Exports) > MaxFunctions || len(m.Elements) > MaxOwnedElementSegments || len(m.Customs) > MaxFunctions {
		return unsupported("closed reference module directory bound")
	}
	if !vector && isCore3RefIsNullExecution(m) {
		return nil
	}
	publicReferenceCallBoundary := !vector && admitPublicReferenceCallBoundary(m)
	publicHostReferenceBoundary := !vector && admitPublicHostReferenceBoundary(m)
	publicDynamicHostReferenceBoundary := !vector && admitPublicDynamicHostReferenceBoundary(m)
	publicReferenceCarrierBoundary := publicReferenceCallBoundary || publicHostReferenceBoundary || publicDynamicHostReferenceBoundary
	counts, err := countClosedReferenceImports(m)
	if err != nil {
		return err
	}
	imported := counts.functions
	if m.UsesCompactImports || len(m.Tables) > 1 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || m.BranchHints != nil {
		return unsupported("closed reference module external state")
	}
	remaining := MaxModuleBytes
	if err := admitClosedReferenceNumericStateWithImports(m, vector, &remaining, counts); err != nil {
		return err
	}
	if err := admitClosedReferenceOwnedTableState(m, &remaining); err != nil {
		return err
	}
	consume := func(n int) bool {
		if n > remaining {
			return false
		}
		remaining -= n
		return true
	}
	if err := chargeDeclaredTypeBytes(m, &remaining); err != nil {
		return err
	}
	types := internalReferenceTypes{
		decoded:                  m.Types,
		decodedSWAR:              vector,
		declarationExtern:        !vector && (admitCore3RefDeclarations(m) || admitCore3BrOnNullDeclarations(m) || admitCore3BrOnNonNullDeclarations(m)),
		declarationNonNullExtern: !vector && (admitCore3RefDeclarations(m) || admitCore3RefAsNonNullDeclarations(m) || admitCore3RefIsNullDeclarations(m)),
		publicReferenceCarriers:  publicReferenceCarrierBoundary && publicReferenceCarrierTarget(&m.Types[0].SubTypes[0].Comp),
	}
	limit := 256
	if vector {
		limit = maxSWARLocals
	}
	for _, group := range m.Types {
		if len(group.SubTypes) != 1 {
			return unsupported("closed reference module recursive group")
		}
		sub := &group.SubTypes[0]
		if !sub.Final || sub.HasPrefix || len(sub.Supers) != 0 || sub.Metadata.Describes.Present() || sub.Metadata.Descriptor.Present() || len(sub.Comp.Fields) != 0 || sub.Comp.Array != (wasm.FieldType{}) {
			return unsupported("closed reference module plain function type")
		}
		if _, _, err := admitDeclaredControlSignature(&sub.Comp, types, true, true, vector); err != nil {
			return err
		}
	}
	var references [MaxFunctions]bool
	// The imported prefix is numeric except for the exact single-host forwarding
	// boundary. No unused import or broader module topology inherits that bridge.
	for _, im := range m.Imports {
		if im.Type.Kind != wasm.ExternFunc {
			continue // Numeric resource shape was checked with owned state.
		}
		// Include the separator in the retained "module.name" binding.
		index := im.Type.FuncType()
		if im.Type != wasm.NewFuncExternType(index) || index.Rec || uint64(index.Index) >= uint64(len(m.Types)) || !consume(6) {
			return unsupported("closed reference imported function binding")
		}
		sig := &m.Types[index.Index].SubTypes[0].Comp
		if vector {
			if err := swarImportSignature(sig); err != nil {
				return err
			}
		} else if err := integerNumericSignature(sig, true); err != nil {
			if (!publicHostReferenceBoundary && !publicDynamicHostReferenceBoundary) || index.Index != 0 || !publicReferenceCarrierTarget(sig) {
				return err
			}
		}
	}
	var importSignatures [MaxHostFunctionImports]*wasm.CompType
	importedOrdinal := 0
	for _, im := range m.Imports {
		if im.Type.Kind == wasm.ExternFunc {
			importSignatures[importedOrdinal] = &m.Types[im.Type.FuncType().Index].SubTypes[0].Comp
			importedOrdinal++
		}
	}
	hasReferenceFunction := false
	for i, index := range m.FuncTypes {
		if index.Rec || uint64(index.Index) >= uint64(len(m.Types)) {
			return unsupported("closed reference module function binding")
		}
		ft := &m.Types[index.Index].SubTypes[0].Comp
		reference, _, err := admitDeclaredControlSignature(ft, types, true, true, vector)
		if err != nil {
			return err
		}
		references[imported+i] = reference
		hasReferenceFunction = hasReferenceFunction || reference
		fn := &m.Code[i]
		if len(fn.BodyBytes) == 0 || len(fn.BodyBytes) > 64<<10 || len(fn.Body.Instrs) != 0 || len(fn.Body.BodyBytes) != 0 || len(fn.Locals.Runs) > limit || fn.LocalDeclBytes > MaxModuleBytes || !consume(len(fn.BodyBytes)) {
			return unsupported("closed reference module byte-backed function bound")
		}
		locals := len(ft.Params)
		// LocalDeclBytes is a source offset, not authority for hand-built
		// run storage. Charge at least the derived encoding, even zero runs.
		localBytes := 1
		if len(fn.Locals.Runs) >= 128 {
			localBytes++
		}
		for _, run := range fn.Locals.Runs {
			if uint64(run.Count) > uint64(limit-locals) {
				return unsupported("closed reference module local bound")
			}
			locals += int(run.Count)
			localBytes += 2 // count and numeric value type
			if run.Count >= 128 {
				localBytes++
			}
			if run.Type.Kind() == wasm.ValRef {
				localBytes += 5
			}
			if !vector && scalarType(run.Type, true) || vector && swarCarrierType(run.Type) {
				continue
			}
			if !internalFunctionReferenceWithTypes(run.Type, true, types) {
				return unsupported("closed reference module local carrier")
			}
		}
		if !consume(max(localBytes, int(fn.LocalDeclBytes))) {
			return unsupported("closed reference module local declaration byte bound")
		}
	}
	if !hasReferenceFunction {
		return unsupported("closed reference module requires internal reference functions")
	}
	numericTarget := func(index uint32) bool { return uint64(index) < uint64(imported+len(m.Code)) && !references[index] }
	descriptorTarget := numericTarget
	if (publicReferenceCallBoundary || publicDynamicHostReferenceBoundary) && publicReferenceCarrierTarget(&m.Types[0].SubTypes[0].Comp) {
		descriptorTarget = func(index uint32) bool { return index == 0 || numericTarget(index) }
	}
	if len(m.Tables) != 0 && m.Tables[0].Init != nil {
		value, err := tableElementExpression(*m.Tables[0].Init)
		if err != nil {
			return err
		}
		if !value.Null && !numericTarget(value.FuncIndex) {
			return unsupported("closed reference table initializer descriptor escape")
		}
	}
	for _, global := range m.Globals {
		_, indexedReference := privateIndexedFunctionGlobal(global, m)
		if wasm.EqualValType(global.Type.Type, wasm.FuncRef) || indexedReference {
			value, err := tableElementExpression(global.Init)
			if err != nil {
				return err
			}
			if !value.Null && !numericTarget(value.FuncIndex) {
				return unsupported("closed reference cell initializer descriptor escape")
			}
		}
	}
	if m.Start != nil {
		index := uint64(*m.Start)
		if index >= uint64(imported+len(m.Code)) || !numericTarget(uint32(index)) || !consume(5) {
			return unsupported("closed reference start requires a bounded numeric function")
		}
		var ft *wasm.CompType
		if index < uint64(imported) {
			ft = importSignatures[index]
		} else {
			ft = &m.Types[m.FuncTypes[int(index)-imported].Index].SubTypes[0].Comp
		}
		if len(ft.Params) != 0 || len(ft.Results) != 0 {
			return unsupported("closed reference start requires a void signature")
		}
	}
	referenceExports := false
	for _, ex := range m.Exports {
		switch ex.Index.Kind {
		case wasm.ExternFunc:
			if uint64(ex.Index.Index) >= uint64(imported+len(m.Code)) {
				return unsupported("closed reference module function export index")
			}
			referenceExports = referenceExports || references[ex.Index.Index]
		case wasm.ExternMem:
			if uint64(ex.Index.Index) >= uint64(counts.memories+len(m.Memories)) {
				return unsupported("closed reference module owned memory export index")
			}
		case wasm.ExternGlobal:
			// Reference cells are private even though their values name numeric slots.
			if uint64(ex.Index.Index) >= uint64(counts.globals+len(m.Globals)) || int(ex.Index.Index) >= counts.globals && m.Globals[int(ex.Index.Index)-counts.globals].Type.Type.Kind() == wasm.ValRef {
				return unsupported("closed reference module owned numeric global export index")
			}
		default:
			return unsupported("closed reference module numeric exports only")
		}
	}
	if referenceExports {
		if len(m.Tables) != 0 {
			return unsupported("public reference wrapper requires table-free ephemeral transport")
		}
		for _, global := range m.Globals {
			if global.Type.Type.Kind() == wasm.ValRef {
				return unsupported("public reference wrapper requires reference-global-free ephemeral transport")
			}
		}
	}
	if err := admitClosedReferenceElements(m, &remaining, descriptorTarget); err != nil {
		return err
	}
	for _, custom := range m.Customs {
		if custom.Name == "metadata.code.branch_hint" {
			return unsupported("closed reference module branch metadata")
		}
	}
	if err := admitSWARDecodedMetadataBudget(m, &remaining); err != nil {
		return err
	}
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	for function, fn := range m.Code {
		r := wasm.ReaderFrom(fn.BodyBytes)
		budget, depth := 0, 0
		for r.HasNext() {
			op, err := r.Byte()
			if err != nil {
				return err
			}
			budget++
			switch op {
			case 0x02, 0x03, 0x04:
				depth++
				if depth > maxControlNesting {
					return unsupported("closed reference module control depth")
				}
			case 0x0b:
				depth--
			case 0x1c, 0x0e:
				probe := r
				count, err := probe.U32()
				if err != nil {
					return err
				}
				if op == 0x1c && count != 1 || op == 0x0e && count > maxTableEntries {
					return unsupported("closed reference module immediate count")
				}
				budget += int(count)
			}
			if budget > integerBudget {
				return unsupported("closed reference module instruction/immediate budget")
			}
			switch op {
			case 0x1f:
				return unsupported("closed reference module exception control is unsupported")
			}
			var imm wasm.InstructionImmediate
			if err := classifier.ClassifyInto(&r, op, &imm); err != nil {
				return err
			}
			if op == 0x24 && uint64(imm.Index) >= uint64(counts.globals) {
				owned := uint64(imm.Index) - uint64(counts.globals)
				if owned >= uint64(len(m.Globals)) || !m.Globals[owned].Type.Mutable {
					return unsupported("closed reference immutable owned global set")
				}
			}
			if op == 0xd2 && !descriptorTarget(imm.Index) {
				return unsupported("closed reference module ref.func escape")
			}
			if referenceExports && (op == 0x14 || op == 0x15) && !publicReferenceCallBoundary && !publicDynamicHostReferenceBoundary {
				return unsupported("public reference wrapper dynamic reference call requires owner scoping")
			}
			if (op == 0x11 || op == 0x13) && (len(m.Tables) != 1 || imm.Index2 != 0) {
				return unsupported("closed reference indirect call requires own table zero")
			}
			if (op == 0x25 || op == 0x26 || imm.Kind == wasm.InstrTableSize) && (len(m.Tables) != 1 || imm.Index != 0) {
				return unsupported("closed reference table operation requires own table zero")
			}
			if op == 0xfc {
				switch imm.Kind {
				case wasm.InstrTableInit, wasm.InstrElemDrop:
					if imm.Kind == wasm.InstrTableInit && len(m.Tables) != 1 || uint64(imm.Index) >= uint64(len(m.Elements)) || imm.Index2 != 0 {
						return unsupported("closed reference element operation requires exact ordinal and init own table")
					}
				case wasm.InstrTableCopy, wasm.InstrTableGrow, wasm.InstrTableFill:
					if len(m.Tables) != 1 || imm.Index != 0 || imm.Index2 != 0 {
						return unsupported("closed reference bulk operation requires own table zero")
					}
					if imm.Kind == wasm.InstrTableGrow && !m.Tables[0].Type.Limits.HasMax && m.Tables[0].Type.Limits.Min > MaxOwnedTableEntries/2 {
						return unsupported("closed reference no-max growth reservation bound")
					}
				}
			}
			if op == 0x14 || op == 0x15 || op == 0x11 || op == 0x13 {
				// Check even dead sites: private functions never become callable
				// descriptor targets, and the existing bridge transports numbers.
				if uint64(imm.Index) >= uint64(len(m.Types)) {
					return unsupported("closed reference numeric descriptor call type index")
				}
				sig := &m.Types[imm.Index].SubTypes[0].Comp
				if vector {
					if err := swarCallSignature(sig); err != nil {
						return err
					}
				} else if err := integerNumericSignature(sig, true); err != nil {
					publicCall := publicReferenceCallBoundary && imm.Index == 0
					publicDynamic := publicDynamicHostReferenceBoundary && imm.Index == 2
					if (!publicCall && !publicDynamic) || (op != 0x14 && op != 0x15) || !publicReferenceCarrierTarget(sig) {
						return err
					}
				}
				if op == 0x15 || op == 0x13 {
					source := &m.Types[m.FuncTypes[function].Index].SubTypes[0].Comp
					if err := admitReferenceTailResults(source, sig); err != nil {
						return err
					}
				}
			}
			if op == 0x12 && uint64(imm.Index) < uint64(imported) {
				// This includes unreachable tails. A private source may have reference
				// parameters/locals, but a numeric imported target must match its results.
				source := &m.Types[m.FuncTypes[function].Index].SubTypes[0].Comp
				if err := admitReferenceTailResults(source, importSignatures[imm.Index]); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
