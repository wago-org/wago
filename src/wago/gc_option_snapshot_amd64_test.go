//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"crypto/sha256"
	"reflect"
	"testing"
)

func TestGCOptionViewPublicMetadataAndSnapshot(t *testing.T) {
	source, _ := iteratorCarrierFixture(16, 0, 0)
	before := sha256.Sum256(source)
	var want []any
	for _, enabled := range []string{"0", "1"} {
		gcOptionTestSwitch(t, enabled)
		c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompileWorkers(1), source)
		if err != nil {
			t.Fatal(err)
		}
		if sha256.Sum256(source) != before {
			c.Close()
			t.Fatal("source bytes changed")
		}
		params, results, err := c.SignatureDescriptor("next")
		if err != nil {
			c.Close()
			t.Fatal(err)
		}
		got := []any{c.TypeDefinitions(), c.ExportedFunctions(), c.ExportedGlobals(), c.ImportedGlobalCount(), c.LocalGlobalCount(), params, results, len(c.Funcs)}
		if enabled == "0" {
			want = got
		} else if !reflect.DeepEqual(want, got) {
			c.Close()
			t.Fatalf("public metadata changed:\n%#v\n%#v", want, got)
		}
		// Instance execution owns its metadata independently of the public view.
		c.Funcs, c.Types, c.Exports, c.Globals, c.Entry, c.InternalEntry = nil, nil, nil, nil, nil, nil
		in, err := Instantiate(c, InstantiateOptions{})
		if err != nil {
			c.Close()
			t.Fatal(err)
		}
		value, err := in.Invoke("run")
		if err != nil || len(value) != 1 || value[0] != 120 {
			in.Close()
			c.Close()
			t.Fatalf("snapshot execution=%v %v", value, err)
		}
		in.Close()
		c.Close()
	}
}
