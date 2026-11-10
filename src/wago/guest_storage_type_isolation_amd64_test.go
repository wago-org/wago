//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestGuestStorageDefinedTypeCannotChangeHostResultAdmission(t *testing.T) {
	rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)))
	t.Cleanup(func() { _ = rt.Close() })
	var child uint64
	var observed ValueTypeDescriptor
	owner, err := rt.NewGCHostFuncRef(slotHostFunc(func(m HostModule, _, results []uint64) {
		storage, ok := m.(GuestStorageHostModule)
		if !ok {
			panic(HostTrap{Err: fmt.Errorf("missing guest storage")})
		}
		if err := storage.WithGuestStorage(func(view GuestStorage) error {
			descriptor, ok := view.DefinedType(3)
			if !ok || len(descriptor.Results) != 1 {
				return fmt.Errorf("missing fixture host result descriptor")
			}
			// This public Go value must not change the live admission requirement.
			descriptor.Results[0].Ref.Heap.TypeIndex = 1
			observed, ok = view.ImportResultType(0)
			if !ok {
				return fmt.Errorf("missing active host result type")
			}
			return nil
		}); err != nil {
			panic(HostTrap{Err: err})
		}
		results[0] = child
	}), FuncSig{Results: []ValType{ValAnyRef}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	module, err := rt.Compile(guestTypeIsolationModule())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = module.Close() })
	in, err := rt.Instantiate(context.Background(), module, WithImports(testImports("host.get", owner)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = in.Close() })
	child = guestTypeIsolationToken(t, in, "child")
	got, err := in.Invoke("host")
	if observed.Ref.Heap.TypeIndex != 0 {
		t.Errorf("public descriptor mutation changed live host result type: %+v", observed)
	}
	if err == nil || !strings.Contains(err.Error(), "required structural result type") {
		t.Errorf("host result after descriptor mutation = %v, %v; want type rejection", got, err)
	}
}

func TestGuestStorageDefinedTypeDetachesAllSlices(t *testing.T) {
	view := &guestStorageView{types: []DefinedTypeDescriptor{{
		Supers:  []uint32{11},
		Params:  []ValueTypeDescriptor{{Kind: ValueTypeI32}},
		Results: []ValueTypeDescriptor{{Kind: ValueTypeI64}},
		Fields:  []FieldTypeDescriptor{{Mutable: true}},
	}}}
	view.active.Store(true)
	got, ok := view.DefinedType(0)
	if !ok {
		t.Fatal("active descriptor unavailable")
	}
	got.Supers[0] = 22
	got.Params[0].Kind = ValueTypeF32
	got.Results[0].Kind = ValueTypeF64
	got.Fields[0].Mutable = false
	again, ok := view.DefinedType(0)
	if !ok || again.Supers[0] != 11 || again.Params[0].Kind != ValueTypeI32 || again.Results[0].Kind != ValueTypeI64 || !again.Fields[0].Mutable {
		t.Fatalf("descriptor slice mutation reached source: %+v", again)
	}
	view.active.Store(false)
	if _, ok := view.DefinedType(0); ok {
		t.Fatal("expired view exposed a descriptor")
	}
}
