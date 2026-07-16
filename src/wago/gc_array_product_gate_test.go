package wago

import (
	"encoding/hex"
	goruntime "runtime"
	"strings"
	"testing"
)

const stagedGCArrayNumericLocalHex = "0061736d01000000019680808000045e7f0160027f7f017f60037f7f7f017f60017f017f0384808080000301020307978080800003036765740000077365745f6765740001036c656e00020ac180808000038c80808000002000fb07002001fb0b000b9c80808000010163002000fb07002103200320012002fb0e0020032001fb0b000b8980808000002000fb0700fb0f0b"

func stagedGCArrayNumericLocalBytes(t testing.TB) []byte {
	t.Helper()
	data, err := hex.DecodeString(stagedGCArrayNumericLocalHex)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestStagedGCArrayProductPlatformBoundsAndIdentityGate(t *testing.T) {
	data := stagedGCArrayNumericLocalBytes(t)
	cfg := NewRuntimeConfig()
	if guardPageBuilt {
		cfg = cfg.WithBoundsChecks(BoundsChecksSignalsBased)
	} else {
		cfg = cfg.WithBoundsChecks(BoundsChecksExplicit)
	}
	features := cfg.frontendFeatures()
	features.TypedFunctionReferences = true
	features.GCArrayProducts = true
	c, err := compileWithFrontendFeatures(cfg, data, features)
	if goruntime.GOOS != "linux" || goruntime.GOARCH != "amd64" {
		if err == nil || !strings.Contains(err.Error(), "unsupported collector-backed array product staged execution on") {
			t.Fatalf("platform compile = %v, want explicit array rejection", err)
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
		if string(unknown[i:i+3]) == "len" {
			unknown[i+2] = 'x'
			break
		}
	}
	if _, err := compileWithFrontendFeatures(cfg, unknown, features); err == nil {
		t.Fatal("unsupported widened array opcode shape unexpectedly compiled")
	}
}
