package wago

import (
	"encoding/hex"
	goruntime "runtime"
	"strings"
	"testing"
)

const stagedGCI31CoreHex = "0061736d010000000199808080000560017f01646c60017f017f6000017f6000027f7f60017f00038880808000070001010202030406918080800002646c004102fb1c0b646c014103fb1c0b07cc8080800007036e65770000056765745f750001056765745f7300020a6765745f752d6e756c6c00030a6765745f732d6e756c6c00040b6765745f676c6f62616c7300050a7365745f676c6f62616c00060ad880808000078680808000002000fb1c0b8880808000002000fb1cfb1e0b8880808000002000fb1cfb1d0b868080800000d06cfb1e0b868080800000d06cfb1d0b8a80808000002300fb1e2301fb1e0b8880808000002000fb1c24010b"

func stagedGCI31CoreBytes(t testing.TB) []byte {
	t.Helper()
	data, err := hex.DecodeString(stagedGCI31CoreHex)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestStagedGCI31ProductPlatformBoundsAndIdentityGate(t *testing.T) {
	data := stagedGCI31CoreBytes(t)
	cfg := NewRuntimeConfig()
	if guardPageBuilt {
		cfg = cfg.WithBoundsChecks(BoundsChecksSignalsBased)
	} else {
		cfg = cfg.WithBoundsChecks(BoundsChecksExplicit)
	}
	features := cfg.frontendFeatures()
	features.TypedFunctionReferences = true
	features.GCI31Products = true
	c, err := compileWithFrontendFeatures(cfg, data, features)
	if goruntime.GOOS != "linux" || goruntime.GOARCH != "amd64" {
		if err == nil || !strings.Contains(err.Error(), "unsupported i31 product staged execution on") {
			t.Fatalf("platform compile = %v, want explicit i31 rejection", err)
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
		if string(unknown[i:i+3]) == "new" {
			unknown[i+2] = 'x'
			break
		}
	}
	widened, err := compileWithFrontendFeatures(cfg, unknown, features)
	if err != nil {
		t.Fatalf("generic i31 product compile = %v", err)
	}
	_ = widened.Close()
}
