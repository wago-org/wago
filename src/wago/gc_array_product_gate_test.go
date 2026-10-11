package wago

import (
	"encoding/hex"
	goruntime "runtime"
	"strings"
	"testing"
)

const stagedGCArrayNumericLocalHex = "0061736d01000000019680808000045e7f0160027f7f017f60037f7f7f017f60017f017f0384808080000301020307978080800003036765740000077365745f6765740001036c656e00020ac180808000038c80808000002000fb07002001fb0b000b9c80808000010163002000fb07002103200320012002fb0e0020032001fb0b000b8980808000002000fb0700fb0f0b"
const stagedGCArrayNumericUniformHex = "0061736d010000000119045e7f0160037f7f7f017f60047f7f7f7f017f60027f7f017f030403010203071703036765740000077365745f6765740001036c656e00020a3b030e0020002001fb06002002fb0b000b1e0101630020002001fb06002104200420022003fb0e0020042002fb0b000b0b0020002001fb0600fb0f0b"

func stagedGCArrayNumericLocalBytes(t testing.TB) []byte {
	t.Helper()
	data, err := hex.DecodeString(stagedGCArrayNumericLocalHex)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func stagedGCArrayNumericUniformBytes(t testing.TB) []byte {
	t.Helper()
	data, err := hex.DecodeString(stagedGCArrayNumericUniformHex)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestStagedGCArrayNumericUniformIdentity(t *testing.T) {
	data := stagedGCArrayNumericUniformBytes(t)
	if product, ok := stagedGCArrayExecutionProduct(data); !ok || product != stagedGCArrayProductNumericUniform {
		t.Fatalf("uniform array product = %v/%v", product, ok)
	}
	near := append([]byte(nil), data...)
	near[len(near)-2] ^= 1
	if product, ok := stagedGCArrayExecutionProduct(near); ok || product != 0 {
		t.Fatalf("mutated uniform array product = %v/%v", product, ok)
	}
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
	if !supportsCompleteCore3Backend(goruntime.GOOS, goruntime.GOARCH) {
		if err == nil || !strings.Contains(err.Error(), "unsupported collector-backed array product staged execution on") {
			t.Fatalf("platform compile = %v, want explicit array rejection", err)
		}
		return
	}
	if err != nil {
		t.Fatalf("supported GC array compile: %v", err)
	}
	_ = c.Close()

	unknown := append([]byte(nil), data...)
	for i := 0; i+3 <= len(unknown); i++ {
		if string(unknown[i:i+3]) == "len" {
			unknown[i+2] = 'x'
			break
		}
	}
	widened, err := compileWithFrontendFeatures(cfg, unknown, features)
	if err != nil {
		t.Fatalf("shape-independent array compile: %v", err)
	}
	_ = widened.Close()
}
