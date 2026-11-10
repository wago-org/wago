package regressiontest

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	TargetEnv   = "WAGO_REGRESSION_PORT_TEST"
	ProtocolEnv = "WAGO_REGRESSION_CHILD_PROTOCOL"
	NonceEnv    = "WAGO_REGRESSION_CHILD_NONCE"
	Protocol    = "2"
	OutcomeMark = "WAGO_REGRESSION_PORT_OUTCOME="
)

type Outcome struct {
	Protocol string `json:"protocol"`
	Target   string `json:"target"`
	Nonce    string `json:"nonce"`
	Status   string `json:"status"`
}

// RunIsolated executes the selected top-level test or subtest in a fresh process.
// It returns true in the parent and in non-selected child subtests, indicating
// that the caller must return without running its body.
func RunIsolated(t *testing.T, timeout time.Duration) bool {
	t.Helper()
	if target := os.Getenv(TargetEnv); target != "" {
		nonce := RequireProtocol(t)
		if target != t.Name() {
			t.Skip("different isolated Regression subtest")
			return true
		}
		t.Cleanup(func() {
			status := "pass"
			if t.Skipped() {
				status = "skip"
			} else if t.Failed() {
				status = "fail"
			}
			payload, err := json.Marshal(Outcome{Protocol: Protocol, Target: t.Name(), Nonce: nonce, Status: status})
			if err != nil {
				t.Errorf("encode Regression child outcome: %v", err)
				return
			}
			fmt.Printf("%s%s\n", OutcomeMark, payload)
		})
		return false
	}
	if timeout <= 0 {
		t.Fatal("non-positive isolated Regression timeout")
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	top := strings.SplitN(t.Name(), "/", 2)[0]
	nonce := NewNonce(t)
	args := []string{"-test.run=^" + regexp.QuoteMeta(top) + "$", "-test.count=1"}
	args = append(args, CoverageArgs()...)
	cmd := exec.CommandContext(ctx, os.Args[0], args...)
	PrepareCommand(cmd)
	cmd.Env = ChildEnvironment(map[string]string{
		TargetEnv:     t.Name(),
		ProtocolEnv:   Protocol,
		NonceEnv:      nonce,
		"WAGO_BOUNDS": ExpectedBounds,
	})
	capture := NewCapture(8<<10, OutcomeMark)
	cmd.Stdout, cmd.Stderr = capture, capture
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("isolated Regression test exceeded %s deadline\n%s", timeout, capture.Output())
	}
	markers := capture.Markers()
	if err != nil {
		t.Fatalf("isolated Regression test failed: %v\n%s", err, capture.Output())
	}
	if err := ValidateOutcome(markers, t.Name(), nonce); err != nil {
		t.Fatalf("isolated Regression outcome: %v\n%s", err, capture.Output())
	}
	return true
}

func ValidateOutcome(markers []string, target, nonce string) error {
	if len(markers) != 1 {
		return fmt.Errorf("emitted %d outcomes, want exactly one", len(markers))
	}
	var outcome Outcome
	if err := DecodeStrictJSON([]byte(markers[0]), &outcome); err != nil {
		return fmt.Errorf("decode outcome: %w", err)
	}
	if outcome.Protocol != Protocol || outcome.Target != target || outcome.Nonce != nonce || outcome.Status != "pass" {
		return fmt.Errorf("identity/status = %+v, want protocol=%q target=%q nonce=%q status=pass", outcome, Protocol, target, nonce)
	}
	return nil
}

func Timeout(t *testing.T, fallback time.Duration) time.Duration {
	t.Helper()
	if raw := os.Getenv("WAGO_REGRESSION_TIMEOUT"); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 {
			t.Fatalf("invalid WAGO_REGRESSION_TIMEOUT %q", raw)
		}
		return parsed
	}
	return fallback
}

func CoverageArgs() []string {
	f := flag.Lookup("test.gocoverdir")
	if f == nil || f.Value.String() == "" {
		return nil
	}
	return []string{"-test.gocoverdir=" + f.Value.String()}
}

func ChildEnvironment(overrides map[string]string) []string {
	preserveKnobs := os.Getenv("WAGO_REGRESSION_PRESERVE_KNOBS") == "1"
	env := make([]string, 0, len(os.Environ())+len(overrides))
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if _, replaced := overrides[key]; replaced {
			continue
		}
		if strings.HasPrefix(key, "WAGO_") {
			if !preserveKnobs || key == "WAGO_BOUNDS" || key == "WAGO_REGRESSION_TIMEOUT" || key == "WAGO_REGRESSION_PRESERVE_KNOBS" || key == TargetEnv || key == ProtocolEnv || key == NonceEnv {
				continue
			}
		}
		env = append(env, item)
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

func RequireProtocol(t *testing.T) string {
	t.Helper()
	if got := os.Getenv(ProtocolEnv); got != Protocol {
		t.Fatalf("invalid Regression child protocol %q", got)
	}
	nonce := os.Getenv(NonceEnv)
	decoded, err := hex.DecodeString(nonce)
	if err != nil || len(decoded) != 16 {
		t.Fatalf("invalid Regression child nonce %q", nonce)
	}
	return nonce
}

func NewNonce(t *testing.T) string {
	t.Helper()
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(nonce[:])
}

func DecodeStrictJSON(data []byte, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}

// Capture bounds retained diagnostics while separately collecting complete,
// small protocol lines. It is safe for concurrent stdout/stderr writes.
type Capture struct {
	mu          sync.Mutex
	limit       int
	prefix      string
	full        []byte
	head        []byte
	tail        []byte
	total       int64
	line        []byte
	lineTooLong bool
	markers     []string
}

func NewCapture(limit int, markerPrefix string) *Capture {
	if limit < 2 {
		limit = 2
	}
	return &Capture{limit: limit, prefix: markerPrefix, full: make([]byte, 0, limit)}
}

func (c *Capture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	oldTotal := c.total
	c.total += int64(len(p))
	if c.full != nil || oldTotal == 0 {
		if c.total <= int64(c.limit) {
			c.full = append(c.full, p...)
		} else {
			c.full = nil
		}
	}
	half := c.limit / 2
	if len(c.head) < half {
		n := half - len(c.head)
		if n > len(p) {
			n = len(p)
		}
		c.head = append(c.head, p[:n]...)
	}
	c.tail = append(c.tail, p...)
	if len(c.tail) > c.limit-half {
		c.tail = append([]byte(nil), c.tail[len(c.tail)-(c.limit-half):]...)
	}
	for _, b := range p {
		if b == '\n' {
			c.finishLine()
			continue
		}
		if !c.lineTooLong {
			if len(c.line) < 64<<10 {
				c.line = append(c.line, b)
			} else {
				c.lineTooLong = true
				c.line = nil
			}
		}
	}
	return len(p), nil
}

func (c *Capture) finishLine() {
	if !c.lineTooLong && strings.HasPrefix(string(c.line), c.prefix) {
		c.markers = append(c.markers, string(c.line[len(c.prefix):]))
	}
	c.line = c.line[:0]
	c.lineTooLong = false
}

func (c *Capture) Markers() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.line) != 0 || c.lineTooLong {
		c.finishLine()
	}
	return append([]string(nil), c.markers...)
}

func (c *Capture) Output() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.full != nil {
		return string(c.full)
	}
	return string(c.head) + fmt.Sprintf("\n... child output truncated (%d bytes total) ...\n", c.total) + string(c.tail)
}
