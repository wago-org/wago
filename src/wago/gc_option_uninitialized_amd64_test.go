//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"errors"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestGCOptionViewRejectsUninitializedCarrier(t *testing.T) {
	for _, name := range []string{"early-tag", "early-payload", "skipped-preheader"} {
		t.Run(name, func(t *testing.T) {
			source, _ := iteratorCarrierFixture(16, 0, 0)
			m, err := wasm.DecodeModule(source)
			if err != nil {
				t.Fatal(err)
			}
			body := m.Code[5].BodyBytes
			if name == "skipped-preheader" {
				step := iteratorFixtureSeq(iteratorFixtureLocal(0), iteratorFixtureCast(3), iteratorFixtureCall(4), iteratorFixtureSet(1))
				// Replace only the first preheader definition with a skipped definition.
				start := -1
				for i := 0; i+len(step) <= len(body); i++ {
					match := true
					for j := range step {
						if body[i+j] != step[j] {
							match = false
							break
						}
					}
					if match {
						start = i
						break
					}
				}
				if start < 0 {
					t.Fatal("fixture preheader missing")
				}
				replacement := iteratorFixtureSeq([]byte{0x41, 0, 0x04, 0x40}, step, []byte{0x0b})
				m.Code[5].BodyBytes = iteratorFixtureSeq(body[:start], replacement, body[start+len(step):])
			} else {
				target, field := uint32(4), uint32(0)
				if name == "early-payload" {
					target, field = 5, 1
				}
				prefix := iteratorFixtureSeq(iteratorFixtureLocal(1), iteratorFixtureCast(target), iteratorFixtureFB(2, target, field), []byte{0x1a})
				m.Code[5].BodyBytes = iteratorFixtureSeq(prefix, body)
			}
			var validation wasm.ValidatedModuleAnalysis
			if err = wasm.ValidateModuleWithAnalysis(m, wasm.ValidationFeatures{GCConstExpr: true, ExtendedConstGlobals: true}, 1, wasm.ValidationLimits{}, &validation); err != nil {
				t.Fatal(err)
			}
			view, err := buildGCOptionBodyView(m)
			if err != nil {
				t.Fatal(err)
			}
			if view != nil {
				t.Fatal("uninitialized carrier admitted")
			}
			binary := optionViewBinary(t, source, m)
			for _, enabled := range []string{"0", "1"} {
				gcOptionTestSwitch(t, enabled)
				c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompileWorkers(1), binary)
				if err != nil {
					t.Fatal(err)
				}
				in, err := Instantiate(c, InstantiateOptions{})
				if err != nil {
					c.Close()
					t.Fatal(err)
				}
				_, err = in.Invoke("run")
				var trap *TrapError
				if !errors.As(err, &trap) || trap.Code != TrapCastFailure {
					in.Close()
					c.Close()
					t.Fatalf("enabled%s got%v want cast failure", enabled, err)
				}
				got, err := in.Invoke("index")
				if err != nil || len(got) != 1 || got[0] != 0 {
					in.Close()
					c.Close()
					t.Fatalf("receiver changed before trap: %v %v", got, err)
				}
				in.Close()
				c.Close()
			}
		})
	}
}
