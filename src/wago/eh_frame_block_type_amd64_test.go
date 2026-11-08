//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestExplicitReferenceBlockTypeRootAdmission(t *testing.T) {
	for _, typeIndex := range []uint32{992, 6} {
		t.Run(fmt.Sprint(typeIndex), func(t *testing.T) {
			compiled, err := Compile(nil, wasmtest.ExplicitReferenceBlockTypeModule(typeIndex))
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			if admission := compiled.GCNativeRootAdmission(); !admission.Required || !admission.Exact {
				t.Fatalf("root admission = %+v, want required exact roots", admission)
			}
		})
	}
}
