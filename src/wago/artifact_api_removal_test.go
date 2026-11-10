//go:build !tinygo && !wago_target_tinygo

package wago

import (
	"reflect"
	"testing"
)

func TestCompiledHasNoArtifactCodec(t *testing.T) {
	typ := reflect.TypeOf((*Compiled)(nil))
	for _, name := range []string{"MarshalBinary", "UnmarshalBinary", "WriteTo", "ReadFrom", "ReadFromWithLimits", "ArtifactSectionSizes"} {
		if _, exists := typ.MethodByName(name); exists {
			t.Errorf("retired artifact method %s is still exposed", name)
		}
	}
}
