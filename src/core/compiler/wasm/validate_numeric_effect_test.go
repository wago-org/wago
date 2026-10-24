package wasm

import (
	"fmt"
	"reflect"
	"testing"
)

func TestDirectNumericEffectsNeedNoPayloadAnalysis(t *testing.T) {
	for _, kind := range simpleOpcode {
		if cat := opEffects[kind].cat; cat >= effUnary && cat <= effConv && validatedFuncNeedsPayloadByKind[kind] {
			t.Fatalf("numeric instruction %v needs payload analysis; update the direct numeric path", kind)
		}
	}
}

func TestNumericEffectMatchesPolymorphicStackValidation(t *testing.T) {
	for kind, e := range opEffects {
		if e.cat == effNone || e.cat > effConv {
			continue
		}
		for _, n := range []int{0, 1, 2, 3} {
			for _, mode := range []string{"concrete", "mismatch", "unknown", "unreachable"} {
				t.Run(fmt.Sprintf("%d/%d/%s", kind, n, mode), func(t *testing.T) {
					a := e.a.valType()
					values := make([]val, n+1)
					values[0] = val{t: F64} // below the current control boundary
					for i := 1; i < len(values); i++ {
						values[i] = val{t: a}
						if mode == "mismatch" {
							values[i].t = ExternRef
						} else if mode == "unknown" {
							values[i].t = Bot
						}
					}
					makeValidator := func() *funcValidator {
						return &funcValidator{vals: append([]val(nil), values...), ctrls: []ctrlFrame{{height: 1, unreachable: mode == "unreachable"}}}
					}
					got, want := makeValidator(), makeValidator()
					gotErr := got.stackEffect(&Instruction{Kind: InstrKind(kind)})
					wantErr := want.popExpect(a)
					if wantErr == nil && (e.cat == effBinary || e.cat == effCompare) {
						wantErr = want.popExpect(a)
					}
					if wantErr == nil {
						result := a
						if e.cat == effTest || e.cat == effCompare {
							result = I32
						} else if e.cat == effConv {
							result = e.b.valType()
						}
						want.push(result)
					}
					if fmt.Sprint(gotErr) != fmt.Sprint(wantErr) || !reflect.DeepEqual(got.vals, want.vals) {
						t.Fatalf("got stack %v, error %v; want stack %v, error %v", got.vals, gotErr, want.vals, wantErr)
					}
				})
			}
		}
	}
}
