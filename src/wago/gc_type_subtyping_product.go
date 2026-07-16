package wago

import (
	"crypto/sha256"
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// stagedGCTypeSubtypingProduct identifies exact collector-free products from
// gc/type-subtyping.wast. Unlike the iteration-37 structural marker, these
// products may carry declared supertype metadata. Their pinned shapes prove that
// no struct/array value is created, stored, imported, exported, or returned.
type stagedGCTypeSubtypingProduct uint8

const (
	stagedGCTypeSubtypingDeclarations stagedGCTypeSubtypingProduct = iota + 1
	stagedGCTypeSubtypingRecursiveFunctions
)

func stagedGCTypeSubtypingProductPinned(data []byte, product stagedGCTypeSubtypingProduct) bool {
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	var pinned stagedGCTypeSubtypingProduct
	switch digest {
	case "aa9754e0665bda5f10ec77a3261759da4b462e813ecf9d0e12ec912acff996d6",
		"ddca4046060c72d14ed416806860b0512b8e34ae2d11555ed88ff8676f6d1871",
		"30ea9ab7a806640c081a4cd0bb68ecd9125f37524b6137f60af89a1c69df2839",
		"76131bcda4dc51168d7c55feabbc7bfb3489dc399b2bb3d0a89a05c56964b5cd",
		"2be8c2ca40f321f5ab956b191184d9b988e1f81963704f316f506bf18235bc9b",
		"ad59582ba55bea406e6c3f6a473bb1fbef90e66275bec4848972483b302ac8c9":
		pinned = stagedGCTypeSubtypingDeclarations
	case "6c5162870907b88c444e61528fe907f280fb2b38b8877bbe98ed58bfebddd496",
		"7421ec51f0e574ac1248b32bc37a7cc0a93445ccf58879e757def2af49039e3a":
		pinned = stagedGCTypeSubtypingRecursiveFunctions
	}
	return pinned == product
}

func stagedGCTypeSubtypingProductShape(m *wasm.Module) (stagedGCTypeSubtypingProduct, error) {
	if m == nil {
		return 0, fmt.Errorf("nil module")
	}
	if len(m.Imports) != 0 || len(m.Globals) != 0 || m.TableCount() != 0 || len(m.Elements) != 0 || m.MemCount() != 0 || len(m.Data) != 0 || m.TagCount() != 0 || m.Start != nil || len(m.Exports) != 0 {
		return 0, fmt.Errorf("type-subtyping products reject imports, globals, tables, elements, memories, data, tags, start, and exports")
	}
	hasHeapType, hasSubtypeMetadata := false, false
	for gi := range m.Types {
		for si := range m.Types[gi].SubTypes {
			st := &m.Types[gi].SubTypes[si]
			if st.Metadata.Describes != nil || st.Metadata.Descriptor != nil {
				return 0, fmt.Errorf("type-subtyping products reject descriptor metadata")
			}
			hasSubtypeMetadata = hasSubtypeMetadata || st.HasPrefix || len(st.Supers) != 0
			switch st.Comp.Kind {
			case wasm.CompFunc:
			case wasm.CompStruct, wasm.CompArray:
				hasHeapType = true
			default:
				return 0, fmt.Errorf("type-subtyping product has unknown composite type %d", st.Comp.Kind)
			}
		}
	}
	if !hasSubtypeMetadata {
		return 0, fmt.Errorf("type-subtyping product requires declared subtype metadata")
	}
	if len(m.FuncTypes) == 0 && len(m.Code) == 0 {
		if !hasHeapType {
			return 0, fmt.Errorf("declaration product requires a struct or array declaration")
		}
		return stagedGCTypeSubtypingDeclarations, nil
	}
	if hasHeapType || len(m.FuncTypes) != 3 || len(m.Code) != 3 {
		return 0, fmt.Errorf("recursive-function product must contain exactly three functions and no heap-object type")
	}
	for i := range m.Code {
		if len(m.Code[i].Locals.Runs) != 0 || !isExactCallsAndLocalGetsBody(m.Code[i].BodyBytes) {
			return 0, fmt.Errorf("recursive-function product function %d has stateful or unsupported instructions", i)
		}
	}
	return stagedGCTypeSubtypingRecursiveFunctions, nil
}

func isExactCallsAndLocalGetsBody(body []byte) bool {
	r := wasm.NewReader(body)
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return false
		}
		switch op {
		case 0x0b: // end
			return r.BytesLeft() == 0
		case 0x10, 0x20: // call, local.get
			if _, err := r.U32(); err != nil {
				return false
			}
		default:
			return false
		}
	}
	return false
}
