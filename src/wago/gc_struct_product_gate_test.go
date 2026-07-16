package wago

import (
	"encoding/hex"
	goruntime "runtime"
	"strings"
	"testing"
)

const (
	stagedGCStructGetOnlyHex        = "0061736d01000000018980808000025f017f016000017f0382808080000101078780808000010367657400000a8f8080800001898080800000fb0100fb0200000b"
	stagedGCStructMutationHex       = "0061736d01000000018e80808000035f017f016000017f60017f017f038380808000020102078d80808000020367657400000373657400010aac8080800002898080800000fb0100fb0200000b988080800001016300fb0100210120012000fb0500002001fb0200000b"
	stagedGCStructRefFieldHex       = "0061736d01000000018b80808000035f005f016300016000000382808080000102078980808000010573746f726500000a9280808000018c8080800000fb0101fb0100fb0501000b"
	stagedGCStructNumericGlobalsHex = "0061736d01000000018780808000015f027f007f0106978080800002640000410a410bfb00000b64000041144115fb00000b078b808080000202673003000267310301"
	stagedGCStructPackedHex         = "0061736d01000000019680808000035f0478007801770077016000027f7f60017f027f7f038b808080000a0101010101010101020206a580808000026400004100410141024103fb00000b64000041fe0141ff0141feff0341ffff03fb00000b07c7818080000c026730030002673103010f6765745f7061636b65645f67305f3000000f6765745f7061636b65645f67315f3000010f6765745f7061636b65645f67305f3100020f6765745f7061636b65645f67315f3100030f6765745f7061636b65645f67305f3200040f6765745f7061636b65645f67315f3200050f6765745f7061636b65645f67305f3300060f6765745f7061636b65645f67315f330007137365745f6765745f7061636b65645f67305f310008137365745f6765745f7061636b65645f67305f3300090acf818080000a8e80808000002300fb0300002300fb0400000b8e80808000002301fb0300002301fb0400000b8e80808000002300fb0300012300fb0400010b8e80808000002301fb0300012301fb0400010b8e80808000002300fb0300022300fb0400020b8e80808000002301fb0300022301fb0400020b8e80808000002300fb0300032300fb0400030b8e80808000002301fb0300032301fb0400030b96808080000023002000fb0500012300fb0300012300fb0400010b96808080000023002000fb0500032300fb0300032300fb0400030b"
	stagedGCStructBasicHex          = "0061736d0100000001a380808000065f037d007d017d006000016e60016400017d6000017d600264007d017d60017d017d038e808080000d01020302030203020304050405069e8080800002640000430000803f43000000404300004040fb00000b640000fb01000b07cb8080800007036e65770000076765745f305f300002096765745f7665635f300004076765745f305f790006096765745f7665635f790008097365745f6765745f79000a097365745f6765745f31000c0ab5818080000d858080800000fb01000b8880808000002000fb0200000b878080800000fb010010010b8880808000002000fb0200000b878080800000fb010010030b8880808000002000fb0200010b878080800000fb010010050b8880808000002000fb0200010b878080800000fb010010070b90808080000020002001fb0500012000fb0200010b898080800000fb0100200010090b90808080000020002001fb0500012000fb0200010b898080800000fb01002000100b0b"
)

func stagedGCStructGetOnlyBytes(t testing.TB) []byte {
	t.Helper()
	data, err := hex.DecodeString(stagedGCStructGetOnlyHex)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func stagedGCStructMutationBytes(t testing.TB) []byte {
	t.Helper()
	data, err := hex.DecodeString(stagedGCStructMutationHex)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestStagedGCStructProductPlatformAndBoundsGate(t *testing.T) {
	data := stagedGCStructGetOnlyBytes(t)
	cfg := NewRuntimeConfig()
	if guardPageBuilt {
		cfg = cfg.WithBoundsChecks(BoundsChecksSignalsBased)
	} else {
		cfg = cfg.WithBoundsChecks(BoundsChecksExplicit)
	}
	features := cfg.frontendFeatures()
	features.TypedFunctionReferences = true
	features.GCStructProducts = true
	c, err := compileWithFrontendFeatures(cfg, data, features)
	if goruntime.GOOS != "linux" || goruntime.GOARCH != "amd64" {
		if err == nil || !strings.Contains(err.Error(), "unsupported collector-backed struct product staged execution on") {
			t.Fatalf("platform compile = %v, want explicit collector-backed struct rejection", err)
		}
		return
	}
	if guardPageBuilt {
		if err == nil || !strings.Contains(err.Error(), "signals-based bounds checks") {
			t.Fatalf("guard compile = %v, want explicit bounds rejection", err)
		}
		return
	}
	if err != nil {
		t.Fatalf("linux/amd64 explicit compile: %v", err)
	}
	_ = c.Close()

	unknown := append([]byte(nil), data...)
	for i := 0; i+3 <= len(unknown); i++ {
		if string(unknown[i:i+3]) == "get" {
			unknown[i+2] = 'x'
			break
		}
	}
	if _, err := compileWithFrontendFeatures(cfg, unknown, features); err == nil || !strings.Contains(err.Error(), "outside the exact pinned product set") {
		t.Fatalf("unknown valid product compile = %v, want exact binary rejection", err)
	}
	refField, err := hex.DecodeString(stagedGCStructRefFieldHex)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compileWithFrontendFeatures(cfg, refField, features); err == nil || !strings.Contains(err.Error(), "outside the exact pinned product set") {
		t.Fatalf("reference-field product compile = %v, want explicit exact-product/barrier gate", err)
	}
}
