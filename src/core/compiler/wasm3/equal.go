package wasm3

func equalValType(a, b ValType) bool {
	if a.Kind != b.Kind || a.Num != b.Num {
		return false
	}
	if a.Kind == ValRef {
		return equalRefType(a.Ref, b.Ref)
	}
	return true
}

func equalRefType(a, b RefType) bool {
	return a.Nullable == b.Nullable && a.Exact == b.Exact && a.Bare == b.Bare && equalHeapType(a.Heap, b.Heap)
}

func equalHeapType(a, b HeapType) bool {
	if a.Kind != b.Kind || a.Abs != b.Abs || a.Type != b.Type {
		return false
	}
	if a.Kind == HeapDefType {
		return a.Def.GroupIndex == b.Def.GroupIndex && a.Def.Index == b.Def.Index
	}
	return true
}

func equalStorageType(a, b StorageType) bool {
	if a.Packed != b.Packed || a.Pack != b.Pack {
		return false
	}
	if !a.Packed && !equalValType(a.Val, b.Val) {
		return false
	}
	return true
}

func equalFieldType(a, b FieldType) bool {
	return a.Mut == b.Mut && equalStorageType(a.Storage, b.Storage)
}

func equalCompType(a, b CompType) bool {
	if a.Kind != b.Kind || len(a.Params) != len(b.Params) || len(a.Results) != len(b.Results) || len(a.Fields) != len(b.Fields) {
		return false
	}
	for i := range a.Params {
		if !equalValType(a.Params[i], b.Params[i]) {
			return false
		}
	}
	for i := range a.Results {
		if !equalValType(a.Results[i], b.Results[i]) {
			return false
		}
	}
	for i := range a.Fields {
		if !equalFieldType(a.Fields[i], b.Fields[i]) {
			return false
		}
	}
	if a.Kind == CompArray && !equalFieldType(a.Array, b.Array) {
		return false
	}
	return true
}
