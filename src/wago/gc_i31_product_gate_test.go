package wago

import (
	"encoding/hex"
	goruntime "runtime"
	"strings"
	"testing"
)

const stagedGCI31CoreHex = "0061736d010000000199808080000560017f01646c60017f017f6000017f6000027f7f60017f00038880808000070001010202030406918080800002646c004102fb1c0b646c014103fb1c0b07cc8080800007036e65770000056765745f750001056765745f7300020a6765745f752d6e756c6c00030a6765745f732d6e756c6c00040b6765745f676c6f62616c7300050a7365745f676c6f62616c00060ad880808000078680808000002000fb1c0b8880808000002000fb1cfb1e0b8880808000002000fb1cfb1d0b868080800000d06cfb1e0b868080800000d06cfb1d0b8a80808000002300fb1e2301fb1e0b8880808000002000fb1c24010b"
const stagedGCI31TableHex = "0061736d01000000019680808000046000017f60017f017f60027f7f017f60037f7f7f0003878080800006000102030303048580808000016c01030a07aa80808000060473697a6500000367657400010467726f7700020466696c6c000304636f7079000404696e6974000509af8080800002060041000b6c0341e707fb1c0b41f806fb1c0b418906fb1c0b056c0341fb00fb1c0b41c803fb1c0b419506fb1c0b0adc8080800006858080800000fc10000b88808080000020002500fb1e0b8b80808000002001fb1c2000fc0f000b8d808080000020002001fb1c2002fc11000b8c8080800000200020012002fc0e00000b8c8080800000200020012002fc0c01000b"
const stagedGCI31TableGlobalInitializerHex = "0061736d010000000186808080000160017f017f028a808080000103656e760167037f000382808080000100048d80808000014000646c0103032300fb1c0b078780808000010367657400000a8e808080000188808080000020002500fb1e0b"
const stagedGCI31GlobalInitializerHex = "0061736d01000000018580808000016000017f028a808080000103656e760167037f000382808080000100068880808000016c002300fb1c0b078780808000010367657400000a8c80808000018680808000002301fb1e0b"
const stagedGCI31AnyGlobalHex = "0061736d01000000018a80808000026000027f7f60017f00038380808000020001069180808000026e0041d209fb1c0b6e0141ae2cfb1c0b079c80808000020b6765745f676c6f62616c7300000a7365745f676c6f62616c00010aa380808000029080808000002300fb176cfb1e2301fb176cfb1e0b8880808000002000fb1c24010b"
const stagedGCI31AnyTableHex = "0061736d01000000019680808000046000017f60017f017f60027f7f017f60037f7f7f0003878080800006000102030303048580808000016e01030a07aa80808000060473697a6500000367657400010467726f7700020466696c6c000304636f7079000404696e6974000509af8080800002060041000b6c0341e707fb1c0b41f806fb1c0b418906fb1c0b056c0341fb00fb1c0b41c803fb1c0b419506fb1c0b0adf8080800006858080800000fc10000b8b808080000020002500fb176cfb1e0b8b80808000002001fb1c2000fc0f000b8d808080000020002001fb1c2002fc11000b8c8080800000200020012002fc0e00000b8c8080800000200020012002fc0c01000b"
const stagedGCI31RefTestGateHex = "0061736d01000000018a80808000026000017f60017f017f038880808000070000010101010107ba8080800007096e756c6c2d646174610000086e756c6c2d693331000103693331000203616e790003026571000406737472756374000505617272617900060a9381808000078d8080800000d06efb146ed06efb156e6a0b8d8080800000d06cfb146cd06cfb156c6a0b9180808000002000fb1cfb146c2000fb1cfb156c6a0b9180808000002000fb1cfb146e2000fb1cfb156e6a0b9180808000002000fb1cfb146d2000fb1cfb156d6a0b9180808000002000fb1cfb146b2000fb1cfb156b6a0b9180808000002000fb1cfb146a2000fb1cfb156a6a0b"

func stagedGCI31CoreBytes(t testing.TB) []byte {
	t.Helper()
	data, err := hex.DecodeString(stagedGCI31CoreHex)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func stagedGCI31TableBytes(t testing.TB) []byte {
	t.Helper()
	data, err := hex.DecodeString(stagedGCI31TableHex)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func stagedGCI31TableGlobalInitializerBytes(t testing.TB) []byte {
	t.Helper()
	data, err := hex.DecodeString(stagedGCI31TableGlobalInitializerHex)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func stagedGCI31GlobalInitializerBytes(t testing.TB) []byte {
	t.Helper()
	data, err := hex.DecodeString(stagedGCI31GlobalInitializerHex)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func stagedGCI31AnyGlobalBytes(t testing.TB) []byte {
	t.Helper()
	data, err := hex.DecodeString(stagedGCI31AnyGlobalHex)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func stagedGCI31AnyTableBytes(t testing.TB) []byte {
	t.Helper()
	data, err := hex.DecodeString(stagedGCI31AnyTableHex)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func stagedGCI31RefTestGateBytes(t testing.TB) []byte {
	t.Helper()
	data, err := hex.DecodeString(stagedGCI31RefTestGateHex)
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
	stagedRV64Core := goruntime.GOOS == "linux" && goruntime.GOARCH == "riscv64"
	if !supportsCompleteCore3Backend(goruntime.GOOS, goruntime.GOARCH) && !stagedRV64Core {
		if err == nil || !strings.Contains(err.Error(), "unsupported i31 product staged execution on") {
			t.Fatalf("platform compile = %v, want explicit i31 rejection", err)
		}
		return
	}
	if err != nil {
		t.Fatalf("supported i31 compile: %v", err)
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
	if stagedRV64Core {
		if widened != nil {
			_ = widened.Close()
		}
		if err == nil {
			t.Fatal("non-pinned i31 source escaped the staged RV64 identity gate")
		}
		return
	}
	if err != nil {
		t.Fatalf("generic i31 product compile = %v", err)
	}
	_ = widened.Close()
}

func TestStagedGCI31TableProductPlatformBoundsAndIdentityGate(t *testing.T) {
	data := stagedGCI31TableBytes(t)
	cfg := compatibilityDefaultConfig()
	features := cfg.frontendFeatures()
	features.TypedFunctionReferences = true
	features.GCI31Products = true
	c, err := compileWithFrontendFeatures(cfg, data, features)
	stagedRV64 := goruntime.GOOS == "linux" && goruntime.GOARCH == "riscv64"
	if !supportsCompleteCore3Backend(goruntime.GOOS, goruntime.GOARCH) && !stagedRV64 {
		if err == nil || !strings.Contains(err.Error(), "unsupported i31 product staged execution on") {
			t.Fatalf("platform compile = %v, want explicit i31-table rejection", err)
		}
		return
	}
	if err != nil {
		t.Fatalf("supported i31-table compile: %v", err)
	}
	_ = c.Close()

	unknown := append([]byte(nil), data...)
	for i := 0; i+4 <= len(unknown); i++ {
		if string(unknown[i:i+4]) == "init" {
			unknown[i+3] = 'x'
			break
		}
	}
	widened, err := compileWithFrontendFeatures(cfg, unknown, features)
	if stagedRV64 {
		if widened != nil {
			_ = widened.Close()
		}
		if err == nil {
			t.Fatal("non-pinned i31-table source escaped the staged RV64 identity gate")
		}
		return
	}
	if err != nil {
		t.Fatalf("generic i31-table product compile = %v", err)
	}
	_ = widened.Close()
}

func TestStagedGCI31TableGlobalInitializerPlatformBoundsAndIdentityGate(t *testing.T) {
	data := stagedGCI31TableGlobalInitializerBytes(t)
	cfg := compatibilityDefaultConfig()
	features := cfg.frontendFeatures()
	features.TypedFunctionReferences = true
	features.GCI31Products = true
	c, err := compileWithFrontendFeatures(cfg, data, features)
	stagedRV64 := goruntime.GOOS == "linux" && goruntime.GOARCH == "riscv64"
	if !supportsCompleteCore3Backend(goruntime.GOOS, goruntime.GOARCH) && !stagedRV64 {
		if err == nil || !strings.Contains(err.Error(), "unsupported i31 product staged execution on") {
			t.Fatalf("platform compile = %v, want explicit i31 table-global initializer rejection", err)
		}
		return
	}
	if err != nil {
		t.Fatalf("supported i31 table-global initializer compile: %v", err)
	}
	_ = c.Close()

	unknown := append([]byte(nil), data...)
	for i := 0; i+3 <= len(unknown); i++ {
		if string(unknown[i:i+3]) == "env" {
			unknown[i+2] = 'x'
			break
		}
	}
	widened, err := compileWithFrontendFeatures(cfg, unknown, features)
	if stagedRV64 {
		if widened != nil {
			_ = widened.Close()
		}
		if err == nil {
			t.Fatal("non-pinned i31 table-global initializer escaped the staged RV64 identity gate")
		}
		return
	}
	if err != nil {
		t.Fatalf("generic i31 table-global initializer compile = %v", err)
	}
	_ = widened.Close()
}

func TestStagedGCI31GlobalInitializerPlatformBoundsAndIdentityGate(t *testing.T) {
	data := stagedGCI31GlobalInitializerBytes(t)
	cfg := compatibilityDefaultConfig()
	features := cfg.frontendFeatures()
	features.TypedFunctionReferences = true
	features.GCI31Products = true
	c, err := compileWithFrontendFeatures(cfg, data, features)
	stagedRV64 := goruntime.GOOS == "linux" && goruntime.GOARCH == "riscv64"
	if !supportsCompleteCore3Backend(goruntime.GOOS, goruntime.GOARCH) && !stagedRV64 {
		if err == nil || !strings.Contains(err.Error(), "unsupported i31 product staged execution on") {
			t.Fatalf("platform compile = %v, want explicit i31 global-global initializer rejection", err)
		}
		return
	}
	if err != nil {
		t.Fatalf("supported i31 global-global initializer compile: %v", err)
	}
	_ = c.Close()

	unknown := append([]byte(nil), data...)
	for i := 0; i+3 <= len(unknown); i++ {
		if string(unknown[i:i+3]) == "env" {
			unknown[i+2] = 'x'
			break
		}
	}
	widened, err := compileWithFrontendFeatures(cfg, unknown, features)
	if stagedRV64 {
		if widened != nil {
			_ = widened.Close()
		}
		if err == nil {
			t.Fatal("non-pinned i31 global-global initializer escaped the staged RV64 identity gate")
		}
		return
	}
	if err != nil {
		t.Fatalf("generic i31 global-global initializer compile = %v", err)
	}
	_ = widened.Close()
}

func TestStagedGCI31AnyGlobalPlatformBoundsAndIdentityGate(t *testing.T) {
	data := stagedGCI31AnyGlobalBytes(t)
	cfg := compatibilityDefaultConfig()
	features := cfg.frontendFeatures()
	features.TypedFunctionReferences = true
	features.GCI31Products = true
	c, err := compileWithFrontendFeatures(cfg, data, features)
	stagedRV64 := goruntime.GOOS == "linux" && goruntime.GOARCH == "riscv64"
	if !supportsCompleteCore3Backend(goruntime.GOOS, goruntime.GOARCH) && !stagedRV64 {
		if err == nil || !strings.Contains(err.Error(), "unsupported i31 product staged execution on") {
			t.Fatalf("platform compile = %v, want explicit i31 anyref-global rejection", err)
		}
		return
	}
	if err != nil {
		t.Fatalf("supported i31 anyref-global compile: %v", err)
	}
	_ = c.Close()

	unknown := append([]byte(nil), data...)
	for i := 0; i+11 <= len(unknown); i++ {
		if string(unknown[i:i+11]) == "get_globals" {
			unknown[i+10] = 'x'
			break
		}
	}
	widened, err := compileWithFrontendFeatures(cfg, unknown, features)
	if stagedRV64 {
		if widened != nil {
			_ = widened.Close()
		}
		if err == nil {
			t.Fatal("non-pinned i31 anyref-global source escaped the staged RV64 identity gate")
		}
		return
	}
	if err != nil {
		t.Fatalf("generic i31 anyref-global compile = %v", err)
	}
	_ = widened.Close()
}

func TestStagedGCI31AnyTablePlatformBoundsAndIdentityGate(t *testing.T) {
	data := stagedGCI31AnyTableBytes(t)
	cfg := compatibilityDefaultConfig()
	features := cfg.frontendFeatures()
	features.TypedFunctionReferences = true
	features.GCI31Products = true
	c, err := compileWithFrontendFeatures(cfg, data, features)
	stagedRV64 := goruntime.GOOS == "linux" && goruntime.GOARCH == "riscv64"
	if !supportsCompleteCore3Backend(goruntime.GOOS, goruntime.GOARCH) && !stagedRV64 {
		if err == nil || !strings.Contains(err.Error(), "unsupported i31 product staged execution on") {
			t.Fatalf("platform compile = %v, want explicit i31 anyref-table rejection", err)
		}
		return
	}
	if err != nil {
		t.Fatalf("supported i31 anyref-table compile: %v", err)
	}
	_ = c.Close()

	unknown := append([]byte(nil), data...)
	for i := 0; i+4 <= len(unknown); i++ {
		if string(unknown[i:i+4]) == "grow" {
			unknown[i+3] = 'x'
			break
		}
	}
	widened, err := compileWithFrontendFeatures(cfg, unknown, features)
	if stagedRV64 {
		if widened != nil {
			_ = widened.Close()
		}
		if err == nil {
			t.Fatal("non-pinned i31 anyref-table source escaped the staged RV64 identity gate")
		}
		return
	}
	if err != nil {
		t.Fatalf("generic i31 anyref-table compile = %v", err)
	}
	_ = widened.Close()
}

func TestStagedGCI31RefTestPlatformBoundsAndIdentityGate(t *testing.T) {
	data := stagedGCI31RefTestGateBytes(t)
	cfg := compatibilityDefaultConfig()
	features := cfg.frontendFeatures()
	features.TypedFunctionReferences = true
	features.GCI31Products = true
	c, err := compileWithFrontendFeatures(cfg, data, features)
	stagedRV64 := goruntime.GOOS == "linux" && goruntime.GOARCH == "riscv64"
	if !supportsCompleteCore3Backend(goruntime.GOOS, goruntime.GOARCH) && !stagedRV64 {
		if err == nil || !strings.Contains(err.Error(), "unsupported i31 product staged execution on") {
			t.Fatalf("platform compile = %v, want explicit i31 ref.test rejection", err)
		}
		return
	}
	if err != nil {
		t.Fatalf("supported i31 ref.test compile: %v", err)
	}
	_ = c.Close()

	unknown := append([]byte(nil), data...)
	for i := 0; i+9 <= len(unknown); i++ {
		if string(unknown[i:i+9]) == "null-data" {
			unknown[i+8] = 'x'
			break
		}
	}
	widened, err := compileWithFrontendFeatures(cfg, unknown, features)
	if stagedRV64 {
		if widened != nil {
			_ = widened.Close()
		}
		if err == nil {
			t.Fatal("non-pinned i31 ref.test source escaped the staged RV64 identity gate")
		}
		return
	}
	if err != nil {
		t.Fatalf("generic i31 ref.test compile = %v", err)
	}
	_ = widened.Close()
}
