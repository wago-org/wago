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
	stagedGCTypeSubtypingRefFuncGlobals
	stagedGCTypeSubtypingRefTestSingle
	stagedGCTypeSubtypingRefTestMulti
	stagedGCTypeSubtypingRefTestDirectionFalse
	stagedGCTypeSubtypingRuntimeCallCast
	stagedGCTypeSubtypingRuntimeFinalityCallCast
	stagedGCTypeSubtypingRuntimeTypedTableCall
	stagedGCTypeSubtypingLinkProvider
	stagedGCTypeSubtypingLinkConsumer
	stagedGCTypeSubtypingFinalityLinkProvider
	stagedGCTypeSubtypingFinalityLinkConsumer
	stagedGCTypeSubtypingStructLinkProvider
	stagedGCTypeSubtypingStructLinkConsumer
	stagedGCTypeSubtypingStructProjectionLinkProvider
	stagedGCTypeSubtypingStructProjectionLinkConsumer
)

func (p stagedGCTypeSubtypingProduct) usesRefTest() bool {
	return p == stagedGCTypeSubtypingRefTestSingle || p == stagedGCTypeSubtypingRefTestMulti || p == stagedGCTypeSubtypingRefTestDirectionFalse
}

func (p stagedGCTypeSubtypingProduct) usesRuntimeFunctionIdentity() bool {
	return p == stagedGCTypeSubtypingRuntimeCallCast || p == stagedGCTypeSubtypingRuntimeFinalityCallCast || p == stagedGCTypeSubtypingRuntimeTypedTableCall
}

func (p stagedGCTypeSubtypingProduct) usesLinkFunctionIdentity() bool {
	return p == stagedGCTypeSubtypingLinkProvider || p == stagedGCTypeSubtypingLinkConsumer || p == stagedGCTypeSubtypingFinalityLinkProvider || p == stagedGCTypeSubtypingFinalityLinkConsumer || p == stagedGCTypeSubtypingStructLinkProvider || p == stagedGCTypeSubtypingStructLinkConsumer || p == stagedGCTypeSubtypingStructProjectionLinkProvider || p == stagedGCTypeSubtypingStructProjectionLinkConsumer
}

func (p stagedGCTypeSubtypingProduct) linkProviderProduct() stagedGCTypeSubtypingProduct {
	switch p {
	case stagedGCTypeSubtypingLinkConsumer:
		return stagedGCTypeSubtypingLinkProvider
	case stagedGCTypeSubtypingFinalityLinkConsumer:
		return stagedGCTypeSubtypingFinalityLinkProvider
	case stagedGCTypeSubtypingStructLinkConsumer:
		return stagedGCTypeSubtypingStructLinkProvider
	case stagedGCTypeSubtypingStructProjectionLinkConsumer:
		return stagedGCTypeSubtypingStructProjectionLinkProvider
	default:
		return 0
	}
}

func (p stagedGCTypeSubtypingProduct) isLinkConsumer() bool {
	return p.linkProviderProduct() != 0
}

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
	case "be069a30cbb75e3ac64dffa08757e2790ab557bc3986faa3440a7de1f87a5171",
		"ecfb84b0d9537fb3455ad6c0bf3c5763ba57de9167fa2e8e83f50ff15a51ac08",
		"4155f7562f90dc7cfa7a1994e2511da5452045eeed10786720355c28fdf27903",
		"6d3373700cb5c07d5c8c30f3c926d20c1cba29b1a0e512db06c7e406d7f71d1b",
		"befde5eb45b4a66d036acfc4f1b69a0b8aabea9df46aa1503b7e7ee73770dd32",
		"a0ba3c1005b6cb73edc08222b5d896276945b0bf1f3b3ff7ef9cdb489341fe08":
		pinned = stagedGCTypeSubtypingRefFuncGlobals
	case "47a4b6080c4c63221e32dd452fd9bc6621c915b3f113e14e46e0f2ff907280d5",
		"97afdb1a9ad042486b76ad816e78a43f933e79b985c6fd20d0658f3b69c6e022",
		"9b8111ee2e3fb91cc7801a63b0a5a8e97eca7b5665f7e6fed5be8a8327534213",
		"60adfeb1cae8b65d159f8c0729630c005f5b530e90d190189487ee241f30c523":
		pinned = stagedGCTypeSubtypingRefTestSingle
	case "5f080674a00a73b3dba391bb1967aa22f4dd6f1b43b9b49aff08528c3305aa6b",
		"b561b7bcd131223f573b787ff002cec3ef83d1cb90fc440ec24d347cc789df1d",
		"893dcf058c5b28436567028ab41bfb409c5f1acc737e764a3dfcc51f6be8200e":
		pinned = stagedGCTypeSubtypingRefTestMulti
	case "2841d098dfca125ccd9c577cf55762744c8a3911a1986f857be48ebc0d51f735",
		"b0797a1825d04be467e336f7f236637184aab41a13de20ff7a06eb1bb7885613":
		pinned = stagedGCTypeSubtypingRefTestDirectionFalse
	case "505e94dbd66fc2e3b5d2d4af76341618b19571074c7b42a551392fd58aa692f3":
		pinned = stagedGCTypeSubtypingRuntimeCallCast
	case "375a327f8469d41d4f15f05109533a90127fc5287414364e227203d7d48e7662":
		pinned = stagedGCTypeSubtypingRuntimeFinalityCallCast
	case "2ad95457821ceb8211d5733fe308f031f1103755733bbf8b5db9c85db0eb6d9b":
		pinned = stagedGCTypeSubtypingRuntimeTypedTableCall
	case "8e9bdbeb27a496328eb9529e0ea629d14a01124b657c4eb0aad74a8bd0f426db":
		pinned = stagedGCTypeSubtypingLinkProvider
	case "ea4d5aaf13a9744bd319a1b33d1ee2303cfaecc84dae73a4179351a6fb91a760",
		"634f7caa3c4e26b757fca7a9a9f8367f99e33304c87f7b2cf6ec7d1e31566535",
		"fe07228154a27a9de4702afb12187709536e894495e8fa2e34a710e2dd7c0b88",
		"24ce2b2eec631ee2946c641e0545d06dc1179e2e9ba646ae59c5d37974111649":
		pinned = stagedGCTypeSubtypingLinkConsumer
	case "dcf54459e9f39087c697c9d9edc0955aabc02eb28e40b65c84291cbe194a9562":
		pinned = stagedGCTypeSubtypingFinalityLinkProvider
	case "ea960ddec4f24c952d26ee7a567309a41c5895cf84690ca120d4577bb4c26e08",
		"7fc43bbbff42ca923db1604d0339cadd21458f5671ea7962d031786e93517996":
		pinned = stagedGCTypeSubtypingFinalityLinkConsumer
	case "ac63802e3827e33389d92ff8a8bd25b6231f1dde96bab5cb77a0e1d094f80e6f":
		pinned = stagedGCTypeSubtypingStructLinkProvider
	case "5f090989edc62437b56b36c69a316cdcfddec4a63d451bd9443ad59da75af0a3":
		pinned = stagedGCTypeSubtypingStructLinkConsumer
	case "8de41fdb1e1b4ef57639e5a6344eed6c13bfb5ada5ea56433bb221f403c56d8e":
		pinned = stagedGCTypeSubtypingStructProjectionLinkProvider
	case "a5d3e6060f52fa0becf68e6e4dd06623df6ecf7bf22bfe5430b484f2adbdf0a2":
		pinned = stagedGCTypeSubtypingStructProjectionLinkConsumer
	}
	return pinned == product
}

func stagedGCTypeSubtypingProductShape(m *wasm.Module) (stagedGCTypeSubtypingProduct, error) {
	if m == nil {
		return 0, fmt.Errorf("nil module")
	}
	if m.TableCount() == 0 && m.MemCount() == 0 && len(m.Globals) == 0 && len(m.Elements) == 0 && len(m.Data) == 0 && m.TagCount() == 0 && m.Start == nil {
		if len(m.Types) == 3 && len(m.Types[0].SubTypes) == 1 && len(m.Types[1].SubTypes) == 1 && len(m.Types[2].SubTypes) == 1 && (m.ImportedFuncCount() != 0 || len(m.Exports) == 3) {
			return stagedGCTypeSubtypingLinkShape(m)
		}
		if len(m.Types) == 2 && len(m.Types[0].SubTypes) == 2 && len(m.Types[1].SubTypes) == 2 && (m.ImportedFuncCount() != 0 || len(m.Exports) == 1) {
			return stagedGCTypeSubtypingStructLinkShape(m)
		}
		if len(m.Types) == 3 && len(m.Types[0].SubTypes) == 2 && len(m.Types[1].SubTypes) == 2 && len(m.Types[2].SubTypes) == 2 && (m.ImportedFuncCount() != 0 || len(m.Exports) == 1) {
			return stagedGCTypeSubtypingStructProjectionLinkShape(m)
		}
		if len(m.Types) == 2 && (m.ImportedFuncCount() != 0 || len(m.Exports) == 2) {
			return stagedGCTypeSubtypingFinalityLinkShape(m)
		}
	}
	if len(m.Elements) != 0 || len(m.Exports) != 0 {
		if m.TableCount() != 0 {
			return stagedGCTypeSubtypingRuntimeCallCastShape(m)
		}
		return stagedGCTypeSubtypingRefTestShape(m)
	}
	if len(m.Imports) != 0 || m.TableCount() != 0 || m.MemCount() != 0 || len(m.Data) != 0 || m.TagCount() != 0 || m.Start != nil {
		return 0, fmt.Errorf("type-subtyping products reject imports, tables, memories, data, tags, and start")
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
	if len(m.Globals) != 0 {
		return stagedGCTypeSubtypingRefFuncGlobalShape(m)
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

func stagedGCTypeSubtypingLinkShape(m *wasm.Module) (stagedGCTypeSubtypingProduct, error) {
	if len(m.Types) != 3 || m.TableCount() != 0 || m.MemCount() != 0 || len(m.Globals) != 0 || len(m.Elements) != 0 || len(m.Data) != 0 || m.TagCount() != 0 || m.Start != nil {
		return 0, fmt.Errorf("link product requires exactly three type groups and no non-function state")
	}
	for i := 0; i < 3; i++ {
		if len(m.Types[i].SubTypes) != 1 {
			return 0, fmt.Errorf("link product type group %d must contain one member", i)
		}
		st := &m.Types[i].SubTypes[0]
		if st.Metadata.Describes != nil || st.Metadata.Descriptor != nil || st.Final || !st.HasPrefix || st.Comp.Kind != wasm.CompFunc || len(st.Comp.Params) != 0 || len(st.Comp.Results) != 1 {
			return 0, fmt.Errorf("link product type %d must be an open zero-parameter single-reference-result function subtype", i)
		}
		if i == 0 {
			if len(st.Supers) != 0 || !wasm.EqualValType(st.Comp.Results[0], wasm.FuncRef) {
				return 0, fmt.Errorf("link product root type must return nullable funcref without a super")
			}
		} else {
			result := st.Comp.Results[0]
			if len(st.Supers) != 1 || st.Supers[0].Rec || st.Supers[0].Index != uint32(i-1) || result.Kind != wasm.ValRef || !result.Ref.Nullable || result.Ref.Exact || result.Ref.Heap.Kind != wasm.HeapTypeIndex || !result.Ref.Heap.Type.Rec || result.Ref.Heap.Type.Index != 0 {
				return 0, fmt.Errorf("link product type %d must extend type %d and return its own nullable reference", i, i-1)
			}
		}
	}
	for source := uint32(0); source < 3; source++ {
		for target := uint32(0); target < 3; target++ {
			actual := wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: source}), false)
			required := wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: target}), false)
			if m.ReferenceTypeSubtype(actual, required) != (source >= target) {
				return 0, fmt.Errorf("link product relation %d <: %d is outside the exact chain", source, target)
			}
		}
	}
	if len(m.Imports) == 0 {
		if len(m.FuncTypes) != 3 || len(m.Code) != 3 || len(m.Exports) != 3 {
			return 0, fmt.Errorf("link provider requires three local functions and three exports")
		}
		wantBodies := []string{"d0700b", "d0010b", "d0020b"}
		for i := 0; i < 3; i++ {
			if m.FuncTypes[i].Rec || m.FuncTypes[i].Index != uint32(i) || len(m.Code[i].Locals.Runs) != 0 || fmt.Sprintf("%x", m.Code[i].BodyBytes) != wantBodies[i] {
				return 0, fmt.Errorf("link provider function %d is outside the exact type/body product", i)
			}
			wantName := fmt.Sprintf("f%d", i)
			ex := m.Exports[i]
			if ex.Name != wantName || ex.Index.Kind != wasm.ExternFunc || ex.Index.Index != uint32(i) {
				return 0, fmt.Errorf("link provider export %d is outside the exact product", i)
			}
		}
		return stagedGCTypeSubtypingLinkProvider, nil
	}
	if len(m.FuncTypes) != 0 || len(m.Code) != 0 || len(m.Exports) != 0 || m.ImportedFuncCount() != len(m.Imports) {
		return 0, fmt.Errorf("link consumer requires only function imports")
	}
	matches := func(wantNames []string, wantTypes []uint32) bool {
		if len(m.Imports) != len(wantNames) || len(wantNames) != len(wantTypes) {
			return false
		}
		for i := range wantNames {
			imp := m.Imports[i]
			if imp.Module != "M" || imp.Name != wantNames[i] || imp.Type.Kind != wasm.ExternFunc || imp.Type.Type.Rec || imp.Type.Type.Index != wantTypes[i] {
				return false
			}
		}
		return true
	}
	if matches([]string{"f0", "f1", "f1", "f2", "f2", "f2"}, []uint32{0, 0, 1, 0, 1, 2}) ||
		matches([]string{"f0"}, []uint32{1}) || matches([]string{"f0"}, []uint32{2}) || matches([]string{"f1"}, []uint32{2}) {
		return stagedGCTypeSubtypingLinkConsumer, nil
	}
	return 0, fmt.Errorf("link consumer import sequence is outside the exact first cluster")
}

func stagedGCTypeSubtypingFinalityLinkShape(m *wasm.Module) (stagedGCTypeSubtypingProduct, error) {
	if len(m.Types) != 2 || m.TableCount() != 0 || m.MemCount() != 0 || len(m.Globals) != 0 || len(m.Elements) != 0 || len(m.Data) != 0 || m.TagCount() != 0 || m.Start != nil {
		return 0, fmt.Errorf("finality link product requires exactly two type groups and no non-function state")
	}
	for i := 0; i < 2; i++ {
		if len(m.Types[i].SubTypes) != 1 {
			return 0, fmt.Errorf("finality link type group %d must contain one member", i)
		}
		st := &m.Types[i].SubTypes[0]
		if st.Metadata.Describes != nil || st.Metadata.Descriptor != nil || len(st.Supers) != 0 || st.Comp.Kind != wasm.CompFunc || len(st.Comp.Params) != 0 || len(st.Comp.Results) != 0 {
			return 0, fmt.Errorf("finality link type %d must be a metadata-free () -> () function without supers", i)
		}
		if i == 0 && (st.Final || !st.HasPrefix) {
			return 0, fmt.Errorf("finality link type 0 must be open")
		}
		if i == 1 && (!st.Final || st.HasPrefix) {
			return 0, fmt.Errorf("finality link type 1 must be final")
		}
	}
	for source := uint32(0); source < 2; source++ {
		for target := uint32(0); target < 2; target++ {
			actual := wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: source}), false)
			required := wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: target}), false)
			if m.ReferenceTypeSubtype(actual, required) != (source == target) {
				return 0, fmt.Errorf("finality link relation %d <: %d is not identity-only", source, target)
			}
		}
	}
	if len(m.Imports) == 0 {
		if len(m.FuncTypes) != 2 || len(m.Code) != 2 || len(m.Exports) != 2 {
			return 0, fmt.Errorf("finality link provider requires two local functions and two exports")
		}
		for i, wantName := range []string{"f1", "f2"} {
			if m.FuncTypes[i].Rec || m.FuncTypes[i].Index != uint32(i) || len(m.Code[i].Locals.Runs) != 0 || !isExactEndBody(m.Code[i].BodyBytes) {
				return 0, fmt.Errorf("finality link provider function %d is outside the exact type/body product", i)
			}
			ex := m.Exports[i]
			if ex.Name != wantName || ex.Index.Kind != wasm.ExternFunc || ex.Index.Index != uint32(i) {
				return 0, fmt.Errorf("finality link provider export %d is outside the exact product", i)
			}
		}
		return stagedGCTypeSubtypingFinalityLinkProvider, nil
	}
	if len(m.FuncTypes) != 0 || len(m.Code) != 0 || len(m.Exports) != 0 || len(m.Imports) != 1 || m.ImportedFuncCount() != 1 {
		return 0, fmt.Errorf("finality link consumer requires exactly one function import")
	}
	imp := m.Imports[0]
	if imp.Module != "M2" || imp.Type.Kind != wasm.ExternFunc || imp.Type.Type.Rec {
		return 0, fmt.Errorf("finality link consumer import is outside the exact M2 function product")
	}
	if imp.Name == "f1" && imp.Type.Type.Index == 1 || imp.Name == "f2" && imp.Type.Type.Index == 0 {
		return stagedGCTypeSubtypingFinalityLinkConsumer, nil
	}
	return 0, fmt.Errorf("finality link consumer import direction is outside the exact inverse pair")
}

func stagedGCTypeSubtypingStructLinkShape(m *wasm.Module) (stagedGCTypeSubtypingProduct, error) {
	if len(m.Types) != 2 || len(m.Types[0].SubTypes) != 2 || len(m.Types[1].SubTypes) != 2 || m.TableCount() != 0 || m.MemCount() != 0 || len(m.Globals) != 0 || len(m.Elements) != 0 || len(m.Data) != 0 || m.TagCount() != 0 || m.Start != nil {
		return 0, fmt.Errorf("struct link product requires exactly two two-member recursive groups and no non-function state")
	}
	for gi := range m.Types {
		for si := range m.Types[gi].SubTypes {
			st := &m.Types[gi].SubTypes[si]
			if st.Metadata.Describes != nil || st.Metadata.Descriptor != nil {
				return 0, fmt.Errorf("struct link type group/member %d/%d carries descriptor metadata", gi, si)
			}
		}
	}
	f := &m.Types[0].SubTypes[0]
	if f.Final || !f.HasPrefix || len(f.Supers) != 0 || f.Comp.Kind != wasm.CompFunc || len(f.Comp.Params) != 0 || len(f.Comp.Results) != 0 {
		return 0, fmt.Errorf("struct link first-group function must be an open () -> () root")
	}
	s := &m.Types[0].SubTypes[1]
	if !s.Final || s.HasPrefix || len(s.Supers) != 0 || s.Comp.Kind != wasm.CompStruct || len(s.Comp.Fields) != 1 {
		return 0, fmt.Errorf("struct link first-group companion must be a final one-field struct")
	}
	field := s.Comp.Fields[0]
	ref := field.Storage.Val
	if field.Mut != wasm.Const || field.Storage.Packed || ref.Kind != wasm.ValRef || ref.Ref.Nullable || ref.Ref.Exact || ref.Ref.Heap.Kind != wasm.HeapTypeIndex || !ref.Ref.Heap.Type.Rec || ref.Ref.Heap.Type.Index != 0 {
		return 0, fmt.Errorf("struct link first-group field must be an immutable non-null reference to recursive member 0")
	}
	g := &m.Types[1].SubTypes[0]
	if g.Final || !g.HasPrefix || len(g.Supers) != 1 || g.Supers[0].Rec || g.Supers[0].Index != 0 || g.Comp.Kind != wasm.CompFunc || len(g.Comp.Params) != 0 || len(g.Comp.Results) != 0 {
		return 0, fmt.Errorf("struct link second-group function must be an open () -> () subtype of flat type 0")
	}
	empty := &m.Types[1].SubTypes[1]
	if !empty.Final || empty.HasPrefix || len(empty.Supers) != 0 || empty.Comp.Kind != wasm.CompStruct || len(empty.Comp.Fields) != 0 {
		return 0, fmt.Errorf("struct link second-group companion must be a final empty struct")
	}
	gRef := wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: 2}), false)
	fRef := wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false)
	if !m.ReferenceTypeSubtype(gRef, fRef) || m.ReferenceTypeSubtype(fRef, gRef) {
		return 0, fmt.Errorf("struct link function relation must be exactly g <: f")
	}
	if len(m.Imports) == 0 {
		if len(m.FuncTypes) != 1 || len(m.Code) != 1 || len(m.Exports) != 1 {
			return 0, fmt.Errorf("struct link provider requires one local function and one export")
		}
		if m.FuncTypes[0].Rec || m.FuncTypes[0].Index != 2 || len(m.Code[0].Locals.Runs) != 0 || !isExactEndBody(m.Code[0].BodyBytes) {
			return 0, fmt.Errorf("struct link provider function is outside the exact type/body product")
		}
		ex := m.Exports[0]
		if ex.Name != "g" || ex.Index.Kind != wasm.ExternFunc || ex.Index.Index != 0 {
			return 0, fmt.Errorf("struct link provider export is outside the exact g product")
		}
		return stagedGCTypeSubtypingStructLinkProvider, nil
	}
	if len(m.FuncTypes) != 0 || len(m.Code) != 0 || len(m.Exports) != 0 || len(m.Imports) != 1 || m.ImportedFuncCount() != 1 {
		return 0, fmt.Errorf("struct link consumer requires exactly one function import")
	}
	imp := m.Imports[0]
	if imp.Module != "M3" || imp.Name != "g" || imp.Type.Kind != wasm.ExternFunc || imp.Type.Type.Rec || imp.Type.Type.Index != 2 {
		return 0, fmt.Errorf("struct link consumer import is outside the exact M3.g product")
	}
	return stagedGCTypeSubtypingStructLinkConsumer, nil
}

func stagedGCTypeSubtypingStructProjectionLinkShape(m *wasm.Module) (stagedGCTypeSubtypingProduct, error) {
	if len(m.Types) != 3 || m.TableCount() != 0 || m.MemCount() != 0 || len(m.Globals) != 0 || len(m.Elements) != 0 || len(m.Data) != 0 || m.TagCount() != 0 || m.Start != nil {
		return 0, fmt.Errorf("struct projection link product requires exactly three type groups and no non-function state")
	}
	for groupIndex := range m.Types {
		group := &m.Types[groupIndex]
		if len(group.SubTypes) != 2 {
			return 0, fmt.Errorf("struct projection link group %d must contain two members", groupIndex)
		}
		for memberIndex := range group.SubTypes {
			st := &group.SubTypes[memberIndex]
			if st.Final || !st.HasPrefix || st.Metadata.Describes != nil || st.Metadata.Descriptor != nil {
				return 0, fmt.Errorf("struct projection link group/member %d/%d must be an open metadata-free subtype", groupIndex, memberIndex)
			}
		}
		f := &group.SubTypes[0]
		if f.Comp.Kind != wasm.CompFunc || len(f.Comp.Params) != 0 || len(f.Comp.Results) != 0 {
			return 0, fmt.Errorf("struct projection link group %d function must be () -> ()", groupIndex)
		}
		s := &group.SubTypes[1]
		wantFields := 1
		if groupIndex == 2 {
			wantFields = 5
		}
		if s.Comp.Kind != wasm.CompStruct || len(s.Comp.Fields) != wantFields {
			return 0, fmt.Errorf("struct projection link group %d struct must contain %d fields", groupIndex, wantFields)
		}
	}
	for groupIndex := 0; groupIndex < 2; groupIndex++ {
		for memberIndex := 0; memberIndex < 2; memberIndex++ {
			if len(m.Types[groupIndex].SubTypes[memberIndex].Supers) != 0 {
				return 0, fmt.Errorf("struct projection link root group/member %d/%d must have no super", groupIndex, memberIndex)
			}
		}
		field := m.Types[groupIndex].SubTypes[1].Comp.Fields[0]
		ref := field.Storage.Val
		if field.Mut != wasm.Const || field.Storage.Packed || ref.Kind != wasm.ValRef || ref.Ref.Nullable || ref.Ref.Exact || ref.Ref.Heap.Kind != wasm.HeapTypeIndex || !ref.Ref.Heap.Type.Rec || ref.Ref.Heap.Type.Index != 0 {
			return 0, fmt.Errorf("struct projection link root group %d field must be an immutable non-null reference to recursive member 0", groupIndex)
		}
	}
	provider := len(m.Imports) == 0
	wantFuncSuper := uint32(2)
	wantStructSuper := uint32(3)
	wantFields := []wasm.TypeIdx{{Index: 0}, {Index: 2}, {Index: 0}, {Index: 2}, {Index: 0, Rec: true}}
	if !provider {
		wantFuncSuper = 0
		wantStructSuper = 1
		wantFields = []wasm.TypeIdx{{Index: 0}, {Index: 0}, {Index: 2}, {Index: 2}, {Index: 0, Rec: true}}
	}
	last := &m.Types[2]
	if supers := last.SubTypes[0].Supers; len(supers) != 1 || supers[0].Rec || supers[0].Index != wantFuncSuper {
		return 0, fmt.Errorf("struct projection link final function must extend flat type %d", wantFuncSuper)
	}
	if supers := last.SubTypes[1].Supers; len(supers) != 1 || supers[0].Rec || supers[0].Index != wantStructSuper {
		return 0, fmt.Errorf("struct projection link final struct must extend flat type %d", wantStructSuper)
	}
	for fieldIndex, want := range wantFields {
		field := last.SubTypes[1].Comp.Fields[fieldIndex]
		ref := field.Storage.Val
		if field.Mut != wasm.Const || field.Storage.Packed || ref.Kind != wasm.ValRef || ref.Ref.Nullable || ref.Ref.Exact || ref.Ref.Heap.Kind != wasm.HeapTypeIndex || ref.Ref.Heap.Type != want {
			return 0, fmt.Errorf("struct projection link final struct field %d is outside the exact ordered projection", fieldIndex)
		}
	}
	if provider {
		if len(m.FuncTypes) != 1 || len(m.Code) != 1 || len(m.Exports) != 1 {
			return 0, fmt.Errorf("struct projection link provider requires one local function and one export")
		}
		if m.FuncTypes[0].Rec || m.FuncTypes[0].Index != 4 || len(m.Code[0].Locals.Runs) != 0 || !isExactEndBody(m.Code[0].BodyBytes) {
			return 0, fmt.Errorf("struct projection link provider function is outside the exact type/body product")
		}
		ex := m.Exports[0]
		if ex.Name != "g" || ex.Index.Kind != wasm.ExternFunc || ex.Index.Index != 0 {
			return 0, fmt.Errorf("struct projection link provider export is outside the exact g product")
		}
		return stagedGCTypeSubtypingStructProjectionLinkProvider, nil
	}
	if len(m.FuncTypes) != 0 || len(m.Code) != 0 || len(m.Exports) != 0 || len(m.Imports) != 1 || m.ImportedFuncCount() != 1 {
		return 0, fmt.Errorf("struct projection link consumer requires exactly one function import")
	}
	imp := m.Imports[0]
	if imp.Module != "M4" || imp.Name != "g" || imp.Type.Kind != wasm.ExternFunc || imp.Type.Type.Rec || imp.Type.Type.Index != 4 {
		return 0, fmt.Errorf("struct projection link consumer import is outside the exact M4.g product")
	}
	return stagedGCTypeSubtypingStructProjectionLinkConsumer, nil
}

func stagedGCTypeSubtypingRefFuncGlobalShape(m *wasm.Module) (stagedGCTypeSubtypingProduct, error) {
	if len(m.FuncTypes) == 0 || len(m.FuncTypes) != len(m.Code) || len(m.FuncTypes) > 2 {
		return 0, fmt.Errorf("ref.func-global product requires one or two local functions")
	}
	if n := len(m.Globals); n != 1 && n != 2 && n != 4 && n != 8 {
		return 0, fmt.Errorf("ref.func-global product has %d globals, want one, two, four, or eight", n)
	}
	for i := range m.Code {
		if len(m.Code[i].Locals.Runs) != 0 || (!isExactEndBody(m.Code[i].BodyBytes) && !isExactUnreachableBody(m.Code[i].BodyBytes)) {
			return 0, fmt.Errorf("ref.func-global product function %d has locals or an unsupported body", i)
		}
	}
	types, err := typeDescriptorsFromWasm(m)
	if err != nil {
		return 0, fmt.Errorf("ref.func-global product type metadata: %w", err)
	}
	for i := range m.Globals {
		g := &m.Globals[i]
		if g.Type.Mutable || !isNonNullIndexedFunctionRef(m, g.Type.Type) {
			return 0, fmt.Errorf("ref.func-global product global %d must be immutable and non-null indexed-function storage", i)
		}
		funcIndex, ok := exactRefFuncBodyIndex(g.Init.BodyBytes)
		if !ok || int(funcIndex) >= len(m.FuncTypes) {
			return 0, fmt.Errorf("ref.func-global product global %d must initialize from a local function", i)
		}
		source, err := valueTypeDescriptorInModule(m, wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(m.FuncTypes[funcIndex]), false)))
		if err != nil {
			return 0, fmt.Errorf("ref.func-global product global %d source type: %w", i, err)
		}
		target, err := valueTypeDescriptorInModule(m, g.Type.Type)
		if err != nil {
			return 0, fmt.Errorf("ref.func-global product global %d target type: %w", i, err)
		}
		if !valueTypeSubtype(source, types, target, types) {
			return 0, fmt.Errorf("ref.func-global product global %d initializer is not a subtype of its declared storage", i)
		}
	}
	return stagedGCTypeSubtypingRefFuncGlobals, nil
}

func stagedGCTypeSubtypingRuntimeCallCastShape(m *wasm.Module) (stagedGCTypeSubtypingProduct, error) {
	if len(m.Imports) != 0 || len(m.Globals) != 0 || m.TableCount() != 1 || m.MemCount() != 0 || len(m.Data) != 0 || m.TagCount() != 0 || m.Start != nil {
		return 0, fmt.Errorf("runtime call/cast product requires one local table and rejects imports, globals, memories, data, tags, and start")
	}
	if len(m.Types) == 2 && len(m.FuncTypes) == 6 && len(m.Code) == 6 && len(m.Elements) == 1 && len(m.Exports) == 4 {
		return stagedGCTypeSubtypingRuntimeFinalityCallCastShape(m)
	}
	if len(m.Types) == 4 && len(m.FuncTypes) == 5 && len(m.Code) == 5 && len(m.Elements) == 1 && len(m.Exports) == 3 {
		return stagedGCTypeSubtypingRuntimeTypedTableCallShape(m)
	}
	if len(m.Types) != 4 || len(m.FuncTypes) != 10 || len(m.Code) != 10 || len(m.Elements) != 1 || len(m.Exports) != 7 {
		return 0, fmt.Errorf("runtime call/cast product requires 4 type groups, 10 functions, 1 element, and 7 exports")
	}
	for i := 0; i < 3; i++ {
		if len(m.Types[i].SubTypes) != 1 {
			return 0, fmt.Errorf("runtime call/cast type group %d must contain one member", i)
		}
		st := &m.Types[i].SubTypes[0]
		if st.Final || !st.HasPrefix || st.Comp.Kind != wasm.CompFunc || len(st.Comp.Params) != 0 || len(st.Comp.Results) != 1 {
			return 0, fmt.Errorf("runtime call/cast type %d must be one open zero-parameter function subtype", i)
		}
		if i == 0 {
			if len(st.Supers) != 0 || !wasm.EqualValType(st.Comp.Results[0], wasm.FuncRef) {
				return 0, fmt.Errorf("runtime call/cast root type must return nullable funcref")
			}
		} else {
			result := st.Comp.Results[0]
			if len(st.Supers) != 1 || st.Supers[0].Rec || st.Supers[0].Index != uint32(i-1) || result.Kind != wasm.ValRef || !result.Ref.Nullable || result.Ref.Heap.Kind != wasm.HeapTypeIndex || !result.Ref.Heap.Type.Rec || result.Ref.Heap.Type.Index != 0 {
				return 0, fmt.Errorf("runtime call/cast type %d must extend type %d and return its own nullable reference", i, i-1)
			}
		}
	}
	runner := &m.Types[3]
	if len(runner.SubTypes) != 1 || !runner.SubTypes[0].Final || runner.SubTypes[0].HasPrefix || len(runner.SubTypes[0].Supers) != 0 || runner.SubTypes[0].Comp.Kind != wasm.CompFunc || len(runner.SubTypes[0].Comp.Params) != 0 || len(runner.SubTypes[0].Comp.Results) != 0 {
		return 0, fmt.Errorf("runtime call/cast runner type must be final () -> ()")
	}
	wantTypes := []uint32{0, 1, 2, 3, 3, 3, 3, 3, 3, 3}
	for i, want := range wantTypes {
		if m.FuncTypes[i].Rec || m.FuncTypes[i].Index != want || len(m.Code[i].Locals.Runs) != 0 {
			return 0, fmt.Errorf("runtime call/cast function %d has unexpected type or locals", i)
		}
	}
	t := m.Tables[0].Type
	if !wasm.EqualValType(wasm.RefVal(t.Ref), wasm.FuncRef) || t.Limits.Addr64 || t.Limits.Min != 3 || t.Limits.Max == nil || *t.Limits.Max != 3 || m.Tables[0].Init != nil {
		return 0, fmt.Errorf("runtime call/cast table must be exact table 3 3 funcref")
	}
	e := &m.Elements[0]
	if e.Mode.Kind != wasm.ElemActive || e.Mode.Table != 0 || !isExactI32ConstZeroBody(e.Mode.Offset.BodyBytes) || e.Kind.Kind != wasm.ElemFuncExprs || len(e.Kind.Exprs) != 3 {
		return 0, fmt.Errorf("runtime call/cast element must initialize three local descriptors at table offset zero")
	}
	for i := range e.Kind.Exprs {
		if !isExactRefFuncBody(e.Kind.Exprs[i].BodyBytes, uint32(i)) {
			return 0, fmt.Errorf("runtime call/cast element %d must name local function %d", i, i)
		}
	}
	wantExports := []string{"run", "fail1", "fail2", "fail3", "fail4", "fail5", "fail6"}
	for i, want := range wantExports {
		ex := m.Exports[i]
		if ex.Name != want || ex.Index.Kind != wasm.ExternFunc || ex.Index.Index != uint32(i+3) {
			return 0, fmt.Errorf("runtime call/cast export %d is outside the exact action surface", i)
		}
	}
	wantBodies := []string{
		"d0700b", "d0010b", "d0020b",
		"027041001100000b027041011100000b027041021100000b02630141011101000b02630141021101000b02630241021102000b02630041002500fb16000b02630041012500fb16000b02630041022500fb16000b02630141012500fb16010b02630141022500fb16010b02630241022500fb16020b0c000b",
		"02630141001101000b0c000b", "02630141001102000b0c000b", "02630141011102000b0c000b",
		"41002500fb16010c000b", "41002500fb16020c000b", "41012500fb16020c000b",
	}
	for i, want := range wantBodies {
		if fmt.Sprintf("%x", m.Code[i].BodyBytes) != want {
			return 0, fmt.Errorf("runtime call/cast function %d body is outside the exact product", i)
		}
	}
	return stagedGCTypeSubtypingRuntimeCallCast, nil
}

func stagedGCTypeSubtypingRuntimeFinalityCallCastShape(m *wasm.Module) (stagedGCTypeSubtypingProduct, error) {
	open := &m.Types[0]
	if len(open.SubTypes) != 1 {
		return 0, fmt.Errorf("runtime finality call/cast open group must contain one member")
	}
	openType := &open.SubTypes[0]
	if openType.Final || !openType.HasPrefix || len(openType.Supers) != 0 || openType.Comp.Kind != wasm.CompFunc || len(openType.Comp.Params) != 0 || len(openType.Comp.Results) != 0 {
		return 0, fmt.Errorf("runtime finality call/cast type 0 must be open () -> ()")
	}
	final := &m.Types[1]
	if len(final.SubTypes) != 1 {
		return 0, fmt.Errorf("runtime finality call/cast final group must contain one member")
	}
	finalType := &final.SubTypes[0]
	if !finalType.Final || finalType.HasPrefix || len(finalType.Supers) != 0 || finalType.Comp.Kind != wasm.CompFunc || len(finalType.Comp.Params) != 0 || len(finalType.Comp.Results) != 0 {
		return 0, fmt.Errorf("runtime finality call/cast type 1 must be final () -> ()")
	}
	for source := uint32(0); source < 2; source++ {
		for target := uint32(0); target < 2; target++ {
			actual := wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: source}), false)
			required := wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: target}), false)
			if m.ReferenceTypeSubtype(actual, required) != (source == target) {
				return 0, fmt.Errorf("runtime finality call/cast relation %d <: %d is not identity-only", source, target)
			}
		}
	}
	wantTypes := []uint32{0, 1, 1, 1, 1, 1}
	for i, want := range wantTypes {
		if m.FuncTypes[i].Rec || m.FuncTypes[i].Index != want || len(m.Code[i].Locals.Runs) != 0 {
			return 0, fmt.Errorf("runtime finality call/cast function %d has unexpected type or locals", i)
		}
	}
	t := m.Tables[0].Type
	if !wasm.EqualValType(wasm.RefVal(t.Ref), wasm.FuncRef) || t.Limits.Addr64 || t.Limits.Min != 2 || t.Limits.Max == nil || *t.Limits.Max != 2 || m.Tables[0].Init != nil {
		return 0, fmt.Errorf("runtime finality call/cast table must be exact table 2 2 funcref")
	}
	e := &m.Elements[0]
	if e.Mode.Kind != wasm.ElemActive || e.Mode.Table != 0 || !isExactI32ConstZeroBody(e.Mode.Offset.BodyBytes) || e.Kind.Kind != wasm.ElemFuncExprs || len(e.Kind.Exprs) != 2 {
		return 0, fmt.Errorf("runtime finality call/cast element must initialize two local descriptors at table offset zero")
	}
	for i := range e.Kind.Exprs {
		if !isExactRefFuncBody(e.Kind.Exprs[i].BodyBytes, uint32(i)) {
			return 0, fmt.Errorf("runtime finality call/cast element %d must name local function %d", i, i)
		}
	}
	wantExports := []string{"fail1", "fail2", "fail3", "fail4"}
	for i, want := range wantExports {
		ex := m.Exports[i]
		if ex.Name != want || ex.Index.Kind != wasm.ExternFunc || ex.Index.Index != uint32(i+2) {
			return 0, fmt.Errorf("runtime finality call/cast export %d is outside the exact action surface", i)
		}
	}
	wantBodies := []string{
		"0b", "0b",
		"024041011100000b0b", "024041001101000b0b",
		"41012500fb16001a0b", "41002500fb16011a0b",
	}
	for i, want := range wantBodies {
		if fmt.Sprintf("%x", m.Code[i].BodyBytes) != want {
			return 0, fmt.Errorf("runtime finality call/cast function %d body is outside the exact product", i)
		}
	}
	return stagedGCTypeSubtypingRuntimeFinalityCallCast, nil
}

func stagedGCTypeSubtypingRuntimeTypedTableCallShape(m *wasm.Module) (stagedGCTypeSubtypingProduct, error) {
	for i := 0; i < 3; i++ {
		if len(m.Types[i].SubTypes) != 1 {
			return 0, fmt.Errorf("runtime typed-table group %d must contain one member", i)
		}
		st := &m.Types[i].SubTypes[0]
		if st.Final || !st.HasPrefix || st.Comp.Kind != wasm.CompFunc || len(st.Comp.Params) != 0 || len(st.Comp.Results) != 0 {
			return 0, fmt.Errorf("runtime typed-table type %d must be open () -> ()", i)
		}
		if i == 0 {
			if len(st.Supers) != 0 {
				return 0, fmt.Errorf("runtime typed-table root type must have no super")
			}
		} else if len(st.Supers) != 1 || st.Supers[0].Rec || st.Supers[0].Index != uint32(i-1) {
			return 0, fmt.Errorf("runtime typed-table type %d must extend type %d", i, i-1)
		}
	}
	runner := &m.Types[3]
	if len(runner.SubTypes) != 1 || !runner.SubTypes[0].Final || runner.SubTypes[0].HasPrefix || len(runner.SubTypes[0].Supers) != 0 || runner.SubTypes[0].Comp.Kind != wasm.CompFunc || len(runner.SubTypes[0].Comp.Params) != 0 || len(runner.SubTypes[0].Comp.Results) != 0 {
		return 0, fmt.Errorf("runtime typed-table runner type must be final () -> ()")
	}
	for source := uint32(0); source < 3; source++ {
		for target := uint32(0); target < 3; target++ {
			actual := wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: source}), false)
			required := wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: target}), false)
			if m.ReferenceTypeSubtype(actual, required) != (source >= target) {
				return 0, fmt.Errorf("runtime typed-table relation %d <: %d is outside the exact chain", source, target)
			}
		}
	}
	wantTypes := []uint32{1, 2, 3, 3, 3}
	for i, want := range wantTypes {
		if m.FuncTypes[i].Rec || m.FuncTypes[i].Index != want || len(m.Code[i].Locals.Runs) != 0 {
			return 0, fmt.Errorf("runtime typed-table function %d has unexpected type or locals", i)
		}
	}
	t := m.Tables[0].Type
	wantTableType := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: 1}), false))
	if !wasm.EqualValType(wasm.RefVal(t.Ref), wantTableType) || t.Limits.Addr64 || t.Limits.Min != 2 || t.Limits.Max == nil || *t.Limits.Max != 2 || m.Tables[0].Init != nil {
		return 0, fmt.Errorf("runtime typed table must be exact table 2 2 (ref null type 1)")
	}
	for _, source := range []uint32{1, 2} {
		actual := wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: source}), false)
		if !m.ReferenceTypeSubtype(actual, t.Ref) {
			return 0, fmt.Errorf("runtime typed-table source type %d is not storable", source)
		}
	}
	if m.ReferenceTypeSubtype(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false), t.Ref) {
		return 0, fmt.Errorf("runtime typed-table root type unexpectedly fits narrower storage")
	}
	e := &m.Elements[0]
	if e.Mode.Kind != wasm.ElemActive || e.Mode.Table != 0 || !isExactI32ConstZeroBody(e.Mode.Offset.BodyBytes) || e.Kind.Kind != wasm.ElemTypedExprs || !wasm.EqualValType(wasm.RefVal(e.Kind.Ref), wantTableType) || len(e.Kind.Exprs) != 2 {
		return 0, fmt.Errorf("runtime typed-table element must initialize two typed local descriptors at table offset zero")
	}
	for i := range e.Kind.Exprs {
		if !isExactRefFuncBody(e.Kind.Exprs[i].BodyBytes, uint32(i)) {
			return 0, fmt.Errorf("runtime typed-table element %d must name local function %d", i, i)
		}
	}
	wantExports := []string{"run", "fail1", "fail2"}
	for i, want := range wantExports {
		ex := m.Exports[i]
		if ex.Name != want || ex.Index.Kind != wasm.ExternFunc || ex.Index.Index != uint32(i+2) {
			return 0, fmt.Errorf("runtime typed-table export %d is outside the exact action surface", i)
		}
	}
	wantBodies := []string{
		"0b", "0b",
		"410011000041011100004100110100410111010041011102000b",
		"41001102000b", "41001103000b",
	}
	for i, want := range wantBodies {
		if fmt.Sprintf("%x", m.Code[i].BodyBytes) != want {
			return 0, fmt.Errorf("runtime typed-table function %d body is outside the exact product", i)
		}
	}
	return stagedGCTypeSubtypingRuntimeTypedTableCall, nil
}

func stagedGCTypeSubtypingRefTestShape(m *wasm.Module) (stagedGCTypeSubtypingProduct, error) {
	if len(m.Imports) != 0 || len(m.Globals) != 0 || m.TableCount() != 0 || m.MemCount() != 0 || len(m.Data) != 0 || m.TagCount() != 0 || m.Start != nil {
		return 0, fmt.Errorf("function ref.test product rejects imports, globals, tables, memories, data, tags, and start")
	}
	if len(m.FuncTypes) != len(m.Code) || (len(m.Code) != 2 && len(m.Code) != 3) {
		return 0, fmt.Errorf("function ref.test product requires two or three local functions")
	}
	runner := len(m.Code) - 1
	for i := range m.Code {
		if len(m.Code[i].Locals.Runs) != 0 {
			return 0, fmt.Errorf("function ref.test product function %d has locals", i)
		}
	}
	for i := 0; i < runner; i++ {
		if runner == 1 && !isExactEndBody(m.Code[i].BodyBytes) {
			return 0, fmt.Errorf("single-result function ref.test source must be empty")
		}
		if runner == 2 && !isExactUnreachableBody(m.Code[i].BodyBytes) {
			return 0, fmt.Errorf("multi-result function ref.test sources must be exactly unreachable")
		}
	}
	if len(m.Elements) != runner {
		return 0, fmt.Errorf("function ref.test product requires one declarative element per source function")
	}
	for i := range m.Elements {
		e := &m.Elements[i]
		if e.Mode.Kind != wasm.ElemDeclarative || e.Kind.Kind != wasm.ElemFuncs || len(e.Kind.Funcs) != 1 || int(e.Kind.Funcs[0]) != i {
			return 0, fmt.Errorf("function ref.test product element %d must name only local function %d", i, i)
		}
	}
	if len(m.Exports) != 1 || m.Exports[0].Name != "run" || m.Exports[0].Index.Kind != wasm.ExternFunc || int(m.Exports[0].Index.Index) != runner {
		return 0, fmt.Errorf("function ref.test product requires only the local runner export")
	}
	pairs, ok := exactRefFuncTestBody(m.Code[runner].BodyBytes)
	if !ok || len(pairs) == 0 {
		return 0, fmt.Errorf("function ref.test runner must contain only ref.func/ref.test pairs")
	}
	ft, ok := m.ResolvedLocalFuncType(runner)
	if !ok || len(ft.Results) != len(pairs) {
		return 0, fmt.Errorf("function ref.test runner results do not match its test count")
	}
	for i, pair := range pairs {
		if int(pair.funcIndex) >= runner {
			return 0, fmt.Errorf("function ref.test pair %d names non-source function %d", i, pair.funcIndex)
		}
		if _, ok := m.TypeFunc(pair.targetType); !ok {
			return 0, fmt.Errorf("function ref.test pair %d target type %d is not a function type", i, pair.targetType)
		}
		if !wasm.EqualValType(ft.Results[i], wasm.I32) {
			return 0, fmt.Errorf("function ref.test runner result %d is not i32", i)
		}
	}
	if len(pairs) == 1 && runner == 1 {
		if stagedGCTypeSubtypingDirectionFalseShape(m, pairs[0]) {
			return stagedGCTypeSubtypingRefTestDirectionFalse, nil
		}
		return stagedGCTypeSubtypingRefTestSingle, nil
	}
	if runner == 1 && len(pairs) == 2 || runner == 2 && (len(pairs) == 4 || len(pairs) == 8) {
		return stagedGCTypeSubtypingRefTestMulti, nil
	}
	return 0, fmt.Errorf("function ref.test product has unsupported %d-source/%d-result shape", runner, len(pairs))
}

// stagedGCTypeSubtypingDirectionFalseShape recognizes the exact open-function
// recursive chain where each later group's second member names the preceding
// group's first member as its super. The tested first member does not inherit
// that sibling edge, so testing it in the reverse target direction must be false.
func stagedGCTypeSubtypingDirectionFalseShape(m *wasm.Module, pair exactRefFuncTestPair) bool {
	graphGroups := len(m.Types) - 1
	if (graphGroups != 2 && graphGroups != 3) || pair.funcIndex != 0 {
		return false
	}
	for groupIndex := 0; groupIndex < graphGroups; groupIndex++ {
		group := &m.Types[groupIndex]
		if len(group.SubTypes) != 2 {
			return false
		}
		for memberIndex := range group.SubTypes {
			st := &group.SubTypes[memberIndex]
			if st.Final || !st.HasPrefix || st.Comp.Kind != wasm.CompFunc || len(st.Comp.Params) != 0 || len(st.Comp.Results) != 0 {
				return false
			}
		}
		if len(group.SubTypes[0].Supers) != 0 || len(group.SubTypes[1].Supers) != 1 {
			return false
		}
		super := group.SubTypes[1].Supers[0]
		if groupIndex == 0 {
			if !super.Rec || super.Index != 0 {
				return false
			}
		} else if super.Rec || super.Index != uint32(2*(groupIndex-1)) {
			return false
		}
	}
	runnerGroup := &m.Types[graphGroups]
	if len(runnerGroup.SubTypes) != 1 {
		return false
	}
	runnerType := &runnerGroup.SubTypes[0]
	if !runnerType.Final || runnerType.HasPrefix || len(runnerType.Supers) != 0 || runnerType.Comp.Kind != wasm.CompFunc || len(runnerType.Comp.Params) != 0 || len(runnerType.Comp.Results) != 1 || !wasm.EqualValType(runnerType.Comp.Results[0], wasm.I32) {
		return false
	}
	sourceType := uint32(2 * (graphGroups - 1))
	targetType := uint32(2 * (graphGroups - 2))
	if len(m.FuncTypes) != 2 || m.FuncTypes[0].Rec || m.FuncTypes[0].Index != sourceType || m.FuncTypes[1].Rec || m.FuncTypes[1].Index != uint32(2*graphGroups) || pair.targetType != targetType {
		return false
	}
	actual := wasm.Ref(false, wasm.IndexedHeap(m.FuncTypes[0]), false)
	required := wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: targetType}), false)
	return !m.ReferenceTypeSubtype(actual, required)
}

type exactRefFuncTestPair struct {
	funcIndex  uint32
	targetType uint32
}

func exactRefFuncTestBody(body []byte) ([]exactRefFuncTestPair, bool) {
	r := wasm.NewReader(body)
	var pairs []exactRefFuncTestPair
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return nil, false
		}
		if op == 0x0b {
			return pairs, r.BytesLeft() == 0
		}
		if op != 0xd2 {
			return nil, false
		}
		funcIndex, err := r.U32()
		if err != nil {
			return nil, false
		}
		op, err = r.Byte()
		if err != nil || op != 0xfb {
			return nil, false
		}
		sub, err := r.U32()
		if err != nil || sub != 20 {
			return nil, false
		}
		target, err := r.S33()
		if err != nil || target < 0 || uint64(target) > uint64(^uint32(0)) {
			return nil, false
		}
		pairs = append(pairs, exactRefFuncTestPair{funcIndex: funcIndex, targetType: uint32(target)})
	}
	return nil, false
}

func exactRefFuncBodyIndex(body []byte) (uint32, bool) {
	r := wasm.NewReader(body)
	op, err := r.Byte()
	if err != nil || op != 0xd2 {
		return 0, false
	}
	index, err := r.U32()
	if err != nil {
		return 0, false
	}
	end, err := r.Byte()
	return index, err == nil && end == 0x0b && r.BytesLeft() == 0
}

func isExactUnreachableBody(body []byte) bool {
	return len(body) == 2 && body[0] == 0x00 && body[1] == 0x0b
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
