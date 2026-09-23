//go:build amd64 && !tinygo

package wago

import "testing"

func TestPluginAVXRequirementsSurviveArtifactRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(*Compiled)
		get  func(*Compiled) bool
	}{
		{"AVX2", func(c *Compiled) { c.requiresAVX2 = true }, (*Compiled).RequiresAVX2},
		{"AVX-512", func(c *Compiled) { c.requiresAVX512 = true }, (*Compiled).RequiresAVX512},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := &Compiled{}
			tc.set(input)
			blob, err := input.MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}
			var loaded Compiled
			if err := loaded.UnmarshalBinary(blob); err != nil {
				t.Fatal(err)
			}
			defer loaded.Close()
			if !tc.get(&loaded) {
				t.Fatalf("%s requirement was lost", tc.name)
			}
		})
	}
}
