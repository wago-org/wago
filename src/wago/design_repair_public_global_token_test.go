package wago

import (
	"context"
	"strings"
	"testing"
)

type repairPublicGlobalWrapper struct{}

func (repairPublicGlobalWrapper) externrefPublicValue() any { return uint64(42) }

func TestDesignRepairPublicGlobalTokenClassification(t *testing.T) {
	rt, other := NewRuntime(), NewRuntime()
	defer rt.Close()
	defer other.Close()
	mod, err := rt.Compile(nullableLocalExternrefGlobalsModule())
	if err != nil {
		t.Fatal(err)
	}
	in, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	g, err := rt.NewExternRefGlobal(NullExternRef(), true)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	ordinary := issueExternref(t, rt, "ordinary")
	foreign := issueExternref(t, other, "foreign")
	expired := issueExternref(t, rt, "expired")
	if !rt.refStore.releaseExternref(expired.token) {
		t.Fatal("release fixture")
	}
	for _, target := range []struct {
		name string
		set  func(Value) error
		get  func() (Value, error)
	}{
		{"local", func(v Value) error { return in.SetGlobalValue("mutable", v) }, func() (Value, error) { return in.GlobalValue("mutable") }},
		{"owned", g.SetValue, g.GetValue},
	} {
		t.Run(target.name, func(t *testing.T) {
			wantInvalid := "invalid externref token"
			// The native host-container adapter has its own established invalid-token wording.
			if target.name == "owned" && compiledConversionStoreHooks(mod.c) {
				wantInvalid = "invalid or foreign externref token"
			}
			for _, fixture := range []struct {
				name  string
				token uint64
			}{
				{"foreign", foreign.token}, {"forged", foreign.token ^ 0x9e3779b97f4a7c15}, {"expired", expired.token},
			} {
				t.Run(fixture.name, func(t *testing.T) {
					err := target.set(ValueOf(ValExternRef, fixture.token))
					if err == nil || !strings.Contains(err.Error(), wantInvalid) {
						t.Fatalf("got %v; want %s", err, wantInvalid)
					}
					got, err := target.get()
					if err != nil || !got.ExternRef().IsNull() {
						t.Fatalf("rejection changed null: %v %v", got, err)
					}
				})
			}
			// Only adapters without opaque publication support reject this authenticated wrapper.
			if !compiledConversionStoreHooks(mod.c) && compiledArchitecture != 4 {
				t.Run("authenticated-wrapper", func(t *testing.T) {
					token, err := rt.refStore.issueExternref(repairPublicGlobalWrapper{})
					if err != nil {
						t.Fatal(err)
					}
					defer rt.refStore.releaseExternref(token)
					if _, ok := rt.refStore.resolveExternref(token); !ok {
						t.Fatal("wrapper unauthenticated")
					}
					err = target.set(ValueOf(ValExternRef, token))
					if err == nil || !strings.Contains(err.Error(), "opaque converted publication is unsupported") {
						t.Fatalf("wrapper classification: %v", err)
					}
					got, err := target.get()
					if err != nil || !got.ExternRef().IsNull() {
						t.Fatalf("wrapper rejection changed null: %v %v", got, err)
					}
				})
			}
			t.Run("ordinary-and-null", func(t *testing.T) {
				if err := target.set(ValueExternRef(ordinary)); err != nil {
					t.Fatal(err)
				}
				got, err := target.get()
				if err != nil || got.ExternRef() != ordinary {
					t.Fatalf("ordinary: %v %v", got, err)
				}
				if err := target.set(ValueExternRef(NullExternRef())); err != nil {
					t.Fatal(err)
				}
				got, err = target.get()
				if err != nil || !got.ExternRef().IsNull() {
					t.Fatalf("clear: %v %v", got, err)
				}
			})
		})
	}
}
