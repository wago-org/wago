//go:build linux && !tinygo

package riscv64

import "github.com/wago-org/wago/src/core/compiler/wasm"

func integerNumericSignature(sig *wasm.CompType, floating bool) error {
	if sig == nil || sig.Kind != wasm.CompFunc || len(sig.Params) > 256 || len(sig.Results) > 256 {
		return unsupported("integer numeric signature shape/bound")
	}
	for _, values := range [][]wasm.ValType{sig.Params, sig.Results} {
		for _, typ := range values {
			if !scalarType(typ, floating) {
				return unsupported("integer numeric callable carrier")
			}
		}
	}
	return nil
}

// Legacy integer probes have only widths. Typed metadata, when present, must
// still describe a numeric callable; a control-only type cannot enter a call ABI.
func integerCallable(sig functionSpec, floating bool) error {
	if len(sig.params) > 256 || len(sig.results) > 256 {
		return unsupported("integer callable arity")
	}
	if sig.signature != nil {
		return integerNumericSignature(sig.signature, floating)
	}
	return nil
}

func chargeControlTypes(budget *int, count int) error {
	if count < 0 || *budget < 0 || *budget > integerBudget-count {
		return unsupported("control signature budget")
	}
	*budget += count
	return nil
}

// Reference-bearing control signatures have their own bounded representation.
// Numeric callable widths and the borrowed signature directory remain unchanged.
// Charge every occurrence, including dead code, before allocating derived flags.
func integerControlSignature(sig functionSpec, types internalReferenceTypes, referencesAllowed, typed, floating bool, budget *int) (params, results, paramRefs, resultRefs []bool, err error) {
	params, results = sig.params, sig.results
	if sig.signature == nil {
		if len(params) > 256 || len(results) > 256 {
			err = unsupported("integer control arity")
			return
		}
		err = chargeControlTypes(budget, len(params)+len(results))
		return
	}
	ft := sig.signature
	if ft.Kind != wasm.CompFunc || len(ft.Params) > 256 || len(ft.Results) > 256 {
		err = unsupported("integer control shape/bound")
		return
	}
	if err = chargeControlTypes(budget, len(ft.Params)+len(ft.Results)); err != nil {
		return
	}
	references := false
	for _, list := range [][]wasm.ValType{ft.Params, ft.Results} {
		for _, typ := range list {
			if scalarType(typ, floating) {
				continue
			}
			if !referencesAllowed || !internalFunctionReferenceWithTypes(typ, typed, types) {
				err = unsupported("integer control carrier")
				return
			}
			references = true
		}
	}
	if !references {
		if len(params) != len(ft.Params) || len(results) != len(ft.Results) {
			err = unsupported("integer numeric control width directory")
		}
		return
	}
	n := len(ft.Params) + len(ft.Results)
	backing := make([]bool, 2*n)
	params, results = backing[:len(ft.Params)], backing[len(ft.Params):n]
	paramRefs, resultRefs = backing[n:n+len(ft.Params)], backing[n+len(ft.Params):]
	for i, typ := range ft.Params {
		paramRefs[i] = typ.Kind() == wasm.ValRef
		params[i] = scalarWide(typ) || paramRefs[i]
	}
	for i, typ := range ft.Results {
		resultRefs[i] = typ.Kind() == wasm.ValRef
		results[i] = scalarWide(typ) || resultRefs[i]
	}
	return
}

func integerSameClasses(a, aRefs, b, bRefs []bool) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] || (len(aRefs) != 0 && aRefs[i]) != (len(bRefs) != 0 && bRefs[i]) {
			return false
		}
	}
	return true
}

func swarControlClass(typ wasm.ValType, types internalReferenceTypes, referencesAllowed, typed bool) swarClass {
	if class := swarClassOf(typ); class != swarInvalid {
		return class
	}
	if referencesAllowed && internalFunctionReferenceWithTypes(typ, typed, types) {
		return swarFuncRef
	}
	return swarInvalid
}

func admitSWARControlSignature(sig *wasm.CompType, types internalReferenceTypes, referencesAllowed, typed bool) error {
	if sig == nil || sig.Kind != wasm.CompFunc || len(sig.Params) > maxSWARLocals || len(sig.Results) > maxSWARLocals {
		return unsupported("SWAR control type shape/bound")
	}
	for _, list := range [][]wasm.ValType{sig.Params, sig.Results} {
		for _, typ := range list {
			if swarControlClass(typ, types, referencesAllowed, typed) == swarInvalid {
				return unsupported("SWAR control type carrier")
			}
		}
	}
	return nil
}

func swarControlSignature(sig *wasm.CompType, types internalReferenceTypes, referencesAllowed, typed bool, budget *int) ([]swarClass, []swarClass, error) {
	if err := admitSWARControlSignature(sig, types, referencesAllowed, typed); err != nil {
		return nil, nil, err
	}
	if err := chargeControlTypes(budget, len(sig.Params)+len(sig.Results)); err != nil {
		return nil, nil, err
	}
	backing := make([]swarClass, len(sig.Params)+len(sig.Results))
	params, results := backing[:len(sig.Params)], backing[len(sig.Params):]
	for i, typ := range sig.Params {
		params[i] = swarControlClass(typ, types, referencesAllowed, typed)
	}
	for i, typ := range sig.Results {
		results[i] = swarControlClass(typ, types, referencesAllowed, typed)
	}
	return params, results, nil
}

// Declaration admission is distinct from callable admission. Reference-valued
// entries can describe only internal controls; all actual functions and call
// sites retain their separate numeric ABI checks. This also classifies unused
// declarations, whose non-null/indexed annotations still require a witness.
func admitDeclaredControlSignature(sig *wasm.CompType, types internalReferenceTypes, referencesAllowed, typed, vector bool) (bool, bool, error) {
	limit := 256
	if vector {
		limit = maxSWARLocals
	}
	if sig == nil || sig.Kind != wasm.CompFunc || len(sig.Params) > limit || len(sig.Results) > limit {
		return false, false, unsupported("declared control signature shape/bound")
	}
	reference, witness := false, false
	for _, values := range [][]wasm.ValType{sig.Params, sig.Results} {
		for _, typ := range values {
			if !vector && scalarType(typ, true) || vector && swarCarrierType(typ) {
				continue
			}
			if !referencesAllowed || !internalFunctionReferenceWithTypes(typ, typed, types) {
				return false, false, unsupported("declared internal reference carrier")
			}
			reference = true
			witness = witness || !typ.Ref().Nullable() || typ.Ref().Heap().Kind() == wasm.HeapTypeIndex
		}
	}
	return reference, witness, nil
}

// Bound the serialized carrier payload for hand-built decoded modules before
// frontend validation expands type metadata. Both source paths share the charge;
// reference encodings conservatively use a prefix plus a maximal S33 heap index.
func chargeDeclaredTypeBytes(m *wasm.Module, remaining *int) error {
	if len(m.Types) > MaxFunctions || *remaining < 0 {
		return unsupported("decoded declared type byte bound")
	}
	flatTypes := 0
	for _, group := range m.Types {
		flatTypes += len(group.SubTypes)
		if flatTypes > MaxFunctions {
			return unsupported("decoded declared type directory bound")
		}
		for subtypeIndex := range group.SubTypes {
			sig := &group.SubTypes[subtypeIndex].Comp
			if len(sig.Params) > 256 || len(sig.Results) > 256 {
				return unsupported("decoded declared type arity")
			}
			charge := 0
			for _, list := range [][]wasm.ValType{sig.Params, sig.Results} {
				for _, value := range list {
					charge++
					if value.Kind() == wasm.ValRef {
						charge += 5
					}
				}
			}
			if charge > *remaining {
				return unsupported("decoded declared type byte bound")
			}
			*remaining -= charge
		}
	}
	return nil
}
