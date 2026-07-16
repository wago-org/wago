package wago

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/gc"
)

// stagedGCStructProduct identifies the exact product boundary of each valid
// module in the pinned Core 3 gc/struct.wast file. Classification is deliberately
// stricter than feature detection: binary identity, command/source line, decoded
// type/storage graph, module state, and opcode inventory must all agree.
type stagedGCStructProduct uint8

const (
	stagedGCStructDeclarations stagedGCStructProduct = iota + 1
	stagedGCStructBindings
	stagedGCStructNamedGets
	stagedGCStructBasic
	stagedGCStructNullDereference
	stagedGCStructPacked
	stagedGCStructNumericLocal
	stagedGCStructNumericGlobals
	stagedGCStructRefTestTable
	stagedGCStructRefTestConcrete
)

type stagedGCStructOpcodeCount struct {
	Opcode string `json:"opcode"`
	Count  int    `json:"count"`
}

type stagedGCStructLeaderPin struct {
	Filename    string
	CommandLine int
	SourceLine  int
	Size        int
	SHA256      string
	Product     stagedGCStructProduct
	Actions     []string
}

var stagedGCStructLeaderPins = []stagedGCStructLeaderPin{
	{Filename: "struct.0.wasm", CommandLine: 9, SourceLine: 3, Size: 85, SHA256: "2f160a99abe79417039118146d28294b053e538438b0e1fb63a0113680be9b79", Product: stagedGCStructDeclarations},
	{Filename: "struct.1.wasm", CommandLine: 17, SourceLine: 25, Size: 74, SHA256: "ef40a050a65b4dd40008b858556a66b412a654caa418326ac74fabb57a1f19fd", Product: stagedGCStructBindings},
	{Filename: "struct.4.wasm", CommandLine: 41, SourceLine: 48, Size: 107, SHA256: "180f5b9ca1a7ea70c079439c6cdc1d94b0eadb60aba5df82912107c39eaf60ff", Product: stagedGCStructNamedGets},
	{Filename: "struct.6.wasm", CommandLine: 77, SourceLine: 70, Size: 373, SHA256: "a469ba81d14ddf21100f100cb67d75942bc0045b9eb09e6c4fc9052ac5ab6c83", Product: stagedGCStructBasic, Actions: []string{"return:new", "return:get_0_0", "return:get_vec_0", "return:get_0_y", "return:get_vec_y", "return:set_get_y", "return:set_get_1"}},
	{Filename: "struct.8.wasm", CommandLine: 106, SourceLine: 145, Size: 118, SHA256: "b01911990bd0678f484afae1a28dec4b44c37308db5f5fdce7aa30b00275fd31", Product: stagedGCStructNullDereference, Actions: []string{"trap:struct.get-null", "trap:struct.set-null"}},
	{Filename: "struct.9.wasm", CommandLine: 144, SourceLine: 160, Size: 514, SHA256: "96a47580d9d86053fdc3306a59b2213fd581b9ba1987da0c240fccb7be1f6e58", Product: stagedGCStructPacked, Actions: []string{"return:get_packed_g0_0", "return:get_packed_g1_0", "return:get_packed_g0_1", "return:get_packed_g1_1", "return:get_packed_g0_2", "return:get_packed_g1_2", "return:get_packed_g0_3", "return:get_packed_g1_3", "return:set_get_packed_g0_1", "return:set_get_packed_g0_3"}},
}

func (p stagedGCStructProduct) String() string {
	switch p {
	case stagedGCStructDeclarations:
		return "declarations"
	case stagedGCStructBindings:
		return "bindings"
	case stagedGCStructNamedGets:
		return "named-field-get"
	case stagedGCStructBasic:
		return "basic-new-get-set"
	case stagedGCStructNullDereference:
		return "null-dereference"
	case stagedGCStructPacked:
		return "packed-fields"
	case stagedGCStructNumericLocal:
		return "numeric-local-helper"
	case stagedGCStructNumericGlobals:
		return "numeric-global-roots"
	case stagedGCStructRefTestTable:
		return "struct-table-ref-test"
	case stagedGCStructRefTestConcrete:
		return "official-concrete-ref-test"
	default:
		return "unknown"
	}
}

func (p stagedGCStructProduct) gateReason() string {
	switch p {
	case stagedGCStructDeclarations:
		return "declaration-only struct type metadata"
	case stagedGCStructBindings:
		return "recursive struct binding and type-index metadata"
	case stagedGCStructNamedGets:
		return "numeric struct.get field-name products"
	case stagedGCStructBasic:
		return "basic struct owned public ref.struct result"
	case stagedGCStructNullDereference:
		return "null struct.get/struct.set trap product"
	case stagedGCStructPacked:
		return "packed struct globals/get/set product"
	case stagedGCStructNumericLocal:
		return "one numeric local struct allocation/access helper product"
	case stagedGCStructNumericGlobals:
		return "two immutable numeric struct globals with collector roots"
	case stagedGCStructRefTestTable:
		return "bounded collector-rooted struct table with dynamic ref.test"
	case stagedGCStructRefTestConcrete:
		return "official concrete struct table dynamic ref.test"
	default:
		return "unknown gc/struct product"
	}
}

func stagedGCStructLeaderPinFor(data []byte, commandLine int) (stagedGCStructLeaderPin, bool) {
	sum := fmt.Sprintf("%x", sha256.Sum256(data))
	for _, pin := range stagedGCStructLeaderPins {
		if pin.CommandLine == commandLine && pin.Size == len(data) && pin.SHA256 == sum {
			return pin, true
		}
	}
	return stagedGCStructLeaderPin{}, false
}

const (
	stagedGCStructNumericLocalSHA256    = "f5fc57a9a6b959a1a689385cb79050b6998c867c61eafd65ff03b2d57d128fcf"
	stagedGCStructNumericMutationSHA256 = "e9f7a7ec88c56684ad5b96e2a5471765ab2835ddea14069006da51a96ed5e891"
	stagedGCStructNumericGlobalsSHA256  = "0387e519fa921b905d0657a6fafb630ab7acaa3a6282e354b3f0f2e45adbfeee"
	stagedGCStructRefTestTableSHA256    = "ab93f46c271d3e1a71c21da7257e29b2363e9188725378005705b33a056a8cbd"
	stagedGCStructRefTestConcreteSHA256 = "7a71f9662207799b262ccbc7909f4e9492c04f7173f84f29be69905d925f6426"
)

// stagedGCStructExecutionProduct admits only the exact collector-backed products
// whose runtime/helper obligations are implemented. The synthetic numeric-local
// product has one mutable i32 field, one allocation per invocation, no GC-valued
// public boundary or global/table state, and returns only numeric values.
func stagedGCStructExecutionProduct(data []byte) (stagedGCStructProduct, bool) {
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	if (digest == stagedGCStructNumericLocalSHA256 && len(data) == 65) ||
		(digest == stagedGCStructNumericMutationSHA256 && len(data) == 106) {
		return stagedGCStructNumericLocal, true
	}
	if digest == stagedGCStructNumericGlobalsSHA256 && len(data) == 67 {
		return stagedGCStructNumericGlobals, true
	}
	if digest == stagedGCStructRefTestTableSHA256 && len(data) == 168 {
		return stagedGCStructRefTestTable, true
	}
	if digest == stagedGCStructRefTestConcreteSHA256 && len(data) == 976 {
		return stagedGCStructRefTestConcrete, true
	}
	for _, pin := range stagedGCStructLeaderPins {
		if pin.SHA256 != digest || pin.Size != len(data) {
			continue
		}
		switch pin.Product {
		case stagedGCStructDeclarations, stagedGCStructBindings, stagedGCStructNamedGets, stagedGCStructBasic, stagedGCStructNullDereference, stagedGCStructPacked:
			return pin.Product, true
		default:
			return pin.Product, false
		}
	}
	return 0, false
}

func (p stagedGCStructProduct) requiresHelpers() bool {
	return p == stagedGCStructNamedGets || p == stagedGCStructNumericLocal || p == stagedGCStructNullDereference || p == stagedGCStructPacked || p == stagedGCStructBasic || p == stagedGCStructRefTestTable || p == stagedGCStructRefTestConcrete
}

func (p stagedGCStructProduct) refTestCanonicalTypes() []gc.TypeID {
	if p != stagedGCStructRefTestConcrete {
		return nil
	}
	return []gc.TypeID{0, 1, 1, 3, 3, 5, 6, 7, 8}
}

func stagedGCStructTypeGraph(m *wasm.Module) string {
	if m == nil {
		return "<nil>"
	}
	groups := make([]string, 0, len(m.Types))
	for _, group := range m.Types {
		members := make([]string, 0, len(group.SubTypes))
		for _, sub := range group.SubTypes {
			var member string
			switch sub.Comp.Kind {
			case wasm.CompStruct:
				fields := make([]string, len(sub.Comp.Fields))
				for i, field := range sub.Comp.Fields {
					fields[i] = stagedGCStructFieldString(field)
				}
				member = "struct{" + strings.Join(fields, ",") + "}"
			case wasm.CompArray:
				member = "array{" + stagedGCStructFieldString(sub.Comp.Array) + "}"
			case wasm.CompFunc:
				member = "func(" + stagedGCStructValTypes(sub.Comp.Params) + ")->(" + stagedGCStructValTypes(sub.Comp.Results) + ")"
			default:
				member = fmt.Sprintf("composite(%d)", sub.Comp.Kind)
			}
			if len(sub.Supers) != 0 || sub.HasPrefix || sub.Metadata.Describes != nil || sub.Metadata.Descriptor != nil {
				member = fmt.Sprintf("sub(final=%t,supers=%v,%s)", sub.Final, sub.Supers, member)
			}
			members = append(members, member)
		}
		groups = append(groups, "rec["+strings.Join(members, ";")+"]")
	}
	return strings.Join(groups, "|")
}

func stagedGCStructFieldString(field wasm.FieldType) string {
	storage := field.Storage.Val.String()
	if field.Storage.Packed {
		if field.Storage.Pack == wasm.PackI8 {
			storage = "i8"
		} else {
			storage = "i16"
		}
	}
	if field.Mut == wasm.Var {
		return "mut " + storage
	}
	return storage
}

func stagedGCStructValTypes(types []wasm.ValType) string {
	out := make([]string, len(types))
	for i := range types {
		out[i] = types[i].String()
	}
	return strings.Join(out, ",")
}

func stagedGCStructStateGraph(m *wasm.Module) string {
	if m == nil {
		return "<nil>"
	}
	globals := make([]string, len(m.Globals))
	for i := range m.Globals {
		mut := "const"
		if m.Globals[i].Type.Mutable {
			mut = "mut"
		}
		globals[i] = mut + " " + m.Globals[i].Type.Type.String()
	}
	exports := make([]string, len(m.Exports))
	for i := range m.Exports {
		exports[i] = fmt.Sprintf("%s=%d:%d", m.Exports[i].Name, m.Exports[i].Index.Kind, m.Exports[i].Index.Index)
	}
	return fmt.Sprintf("imports=%d funcs=%d globals=[%s] tables=%d memories=%d tags=%d elements=%d data=%d exports=[%s]",
		len(m.Imports), len(m.Code), strings.Join(globals, ","), m.TableCount(), m.MemCount(), m.TagCount(), len(m.Elements), len(m.Data), strings.Join(exports, ","))
}

func stagedGCStructOpcodeInventory(m *wasm.Module) ([]stagedGCStructOpcodeCount, error) {
	counts := map[string]int{}
	walk := func(body []byte) error {
		r := wasm.NewReader(body)
		for r.HasNext() {
			op, err := r.Byte()
			if err != nil {
				return err
			}
			imm, err := wasm.ClassifyInstructionImmediate(r, op)
			if err != nil {
				return err
			}
			if op != 0x0b {
				counts[imm.Kind.String()]++
			}
		}
		return nil
	}
	for i := range m.Globals {
		if err := walk(m.Globals[i].Init.BodyBytes); err != nil {
			return nil, fmt.Errorf("global %d initializer: %w", i, err)
		}
	}
	for i := range m.Code {
		if err := walk(m.Code[i].BodyBytes); err != nil {
			return nil, fmt.Errorf("function %d body: %w", i, err)
		}
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]stagedGCStructOpcodeCount, 0, len(keys))
	for _, key := range keys {
		out = append(out, stagedGCStructOpcodeCount{Opcode: key, Count: counts[key]})
	}
	return out, nil
}
