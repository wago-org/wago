//go:build linux && riscv64 && !tinygo && !wago_target_tinygo && !wago_precompiled && !wago_guardpage

package wago

import "testing"

func TestRV64ThreadedArtifactSchema(t *testing.T) {
	blob := rv64PreparePortableFeature(t, "threads")
	var decoded Compiled
	err := unmarshalCompiled(&decoded, blob[5:])
	t.Logf("decode=%v features=%s bounds=%v memories=%d imports=%d tables=%d tagimports=%d active=%d passive=%d elems=%d/%d slots=%d header=%t wait=%t profile=%t memory=%+v", err, decoded.requiredFeatures, decoded.boundsMode, decoded.memoryCount(), decoded.NumImports, decoded.tableCount(), decoded.tagImportCount(), decoded.activeDataCount(), len(decoded.PassiveData), len(decoded.Elems), len(decoded.passiveElems), decoded.syncHostSlots, decoded.needsFuncRefContextHeader, decoded.usesAtomicWaitHelpers(), rv64ThreadedMemoryProfile(&decoded), decoded.memoryDef(0))
	if err != nil {
		t.Fatal(err)
	}
	if !rv64ThreadedMemoryProfile(&decoded) {
		t.Fatal("decoded thread schema rejected")
	}
	c, err := LoadTrustedArtifact(blob)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if !rv64ArtifactContractValid(c.executionView()) {
		t.Fatal("missing executable metadata binding")
	}
}
