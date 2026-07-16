package wago

import (
	"encoding/hex"
	goruntime "runtime"
	"strings"
	"testing"
)

const (
	stagedGCStructGetOnlyHex  = "0061736d01000000018980808000025f017f016000017f0382808080000101078780808000010367657400000a8f8080800001898080800000fb0100fb0200000b"
	stagedGCStructMutationHex = "0061736d01000000018e80808000035f017f016000017f60017f017f038380808000020102078d80808000020367657400000373657400010aac8080800002898080800000fb0100fb0200000b988080800001016300fb0100210120012000fb0500002001fb0200000b"
	stagedGCStructRefFieldHex = "0061736d01000000018b80808000035f005f016300016000000382808080000102078980808000010573746f726500000a9280808000018c8080800000fb0101fb0100fb0501000b"
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
