//go:build amd64 && tinygo && wago_minimal

package wago

import (
	"encoding/binary"
	"strings"
	"testing"
)

func TestMinimalTinyGoRejectsAVXArtifacts(t *testing.T) {
	for _, feature := range []uint64{compiledCPUFeatureAVX2, compiledCPUFeatureAVX512} {
		blob, err := (&Compiled{}).MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		required := binary.LittleEndian.Uint64(blob[len(blob)-9 : len(blob)-1])
		binary.LittleEndian.PutUint64(blob[len(blob)-9:len(blob)-1], required|feature)
		var loaded Compiled
		if err := loaded.UnmarshalBinary(blob); err == nil || !strings.Contains(err.Error(), "unknown required feature bits") {
			t.Fatalf("feature %#x: error = %v", feature, err)
		}
	}
}
