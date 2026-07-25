//go:build ((linux && (amd64 || arm64)) || (darwin && arm64)) && !tinygo

package wago

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

const (
	wasmtimePortTestEnvInternal      = "WAGO_WASMTIME_PORT_TEST"
	wasmtimeChildProtocolEnvInternal = "WAGO_WASMTIME_CHILD_PROTOCOL"
	wasmtimeChildNonceEnvInternal    = "WAGO_WASMTIME_CHILD_NONCE"
	wasmtimeChildProtocolInternal    = "1"
)

func runWasmtimeIsolatedPortTest(t *testing.T) bool {
	t.Helper()
	if target := os.Getenv(wasmtimePortTestEnvInternal); target != "" {
		requireWasmtimePortChildProtocol(t)
		if target != t.Name() {
			t.Skip("different isolated Wasmtime subtest")
			return true
		}
		return false
	}

	timeout := 30 * time.Second
	if raw := os.Getenv("WAGO_WASMTIME_TIMEOUT"); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 {
			t.Fatalf("invalid WAGO_WASMTIME_TIMEOUT %q", raw)
		}
		timeout = parsed
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	top := strings.SplitN(t.Name(), "/", 2)[0]
	nonce := newWasmtimePortChildNonce(t)
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^"+regexp.QuoteMeta(top)+"$", "-test.count=1")
	cmd.Env = wasmtimePortChildEnvironment(map[string]string{
		wasmtimePortTestEnvInternal:      t.Name(),
		wasmtimeChildProtocolEnvInternal: wasmtimeChildProtocolInternal,
		wasmtimeChildNonceEnvInternal:    nonce,
	})
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("isolated Wasmtime test exceeded %s deadline\n%s", timeout, truncateWasmtimePortChildOutput(out))
	}
	if err != nil {
		t.Fatalf("isolated Wasmtime test failed: %v\n%s", err, truncateWasmtimePortChildOutput(out))
	}
	return testing.CoverMode() == ""
}

func requireWasmtimePortChildProtocol(t *testing.T) {
	t.Helper()
	if got := os.Getenv(wasmtimeChildProtocolEnvInternal); got != wasmtimeChildProtocolInternal {
		t.Fatalf("invalid Wasmtime child protocol %q", got)
	}
	nonce := os.Getenv(wasmtimeChildNonceEnvInternal)
	decoded, err := hex.DecodeString(nonce)
	if err != nil || len(decoded) != 16 {
		t.Fatalf("invalid Wasmtime child nonce %q", nonce)
	}
}

func newWasmtimePortChildNonce(t *testing.T) string {
	t.Helper()
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(nonce[:])
}

func wasmtimePortChildEnvironment(overrides map[string]string) []string {
	blocked := map[string]bool{
		"WAGO_WASMTIME_FIXTURE":          true,
		wasmtimePortTestEnvInternal:      true,
		wasmtimeChildProtocolEnvInternal: true,
		wasmtimeChildNonceEnvInternal:    true,
	}
	env := make([]string, 0, len(os.Environ())+len(overrides))
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if !blocked[key] {
			env = append(env, item)
		}
	}
	keys := make([]string, 0, len(overrides))
	for key := range overrides {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		env = append(env, key+"="+overrides[key])
	}
	return env
}

func truncateWasmtimePortChildOutput(out []byte) string {
	const limit = 8 << 10
	if len(out) <= limit {
		return string(out)
	}
	half := limit / 2
	return string(out[:half]) + "\n... child output truncated ...\n" + string(out[len(out)-half:])
}
