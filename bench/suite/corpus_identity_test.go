package wagobench

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/wago-org/wago/tests/support/wasmtest"
)

// This protocol qualifies only the existing single-request corpus child lane.
// Native/compiler-path attribution and other engine adapters remain separate.
const corpusCompletionLimit = 64 << 10
const corpusCompletionProfile = "corpus-exact-slots-v1"

type corpusCompletion struct {
	Schema                          int
	ID, Stage, Export, Init, Bounds string
	LoadedSHA256                    string
	Target, GoVersion, Profile      string
	Calls, InitCalls                int
	Args, Results                   []uint64
}

func corpusCompletionHeader(m corpusModule, stage, export, bounds string) corpusCompletion {
	return corpusCompletion{Schema: 1, ID: m.ID, Stage: stage, Export: export,
		Init: m.Init, Bounds: bounds, LoadedSHA256: m.ArtifactSHA256,
		Target: runtime.GOOS + "/" + runtime.GOARCH, GoVersion: runtime.Version(), Profile: corpusCompletionProfile}
}

func corpusLoadedCompletion(m corpusModule, stage, export, bounds string) corpusCompletion {
	r := corpusCompletionHeader(m, stage, export, bounds)
	r.LoadedSHA256 = fmt.Sprintf("%x", sha256.Sum256(m.bytes))
	return r
}

func corpusExpectedCompletion(tb testing.TB, m corpusModule, stage, export, bounds string) corpusCompletion {
	tb.Helper()
	want := corpusCompletionHeader(m, stage, export, bounds)
	// The parent uses the independently pinned catalog digest, not a child's claim.
	want.LoadedSHA256 = m.ArtifactSHA256
	if stage == "Exec" {
		for _, invocation := range m.Exec {
			if invocation.Export != export {
				continue
			}
			want.Calls = 1
			if m.Init != "" {
				want.InitCalls = 1
			}
			for _, arg := range invocation.Args {
				want.Args = append(want.Args, uint64(uint32(arg)))
			}
			want.Results = slices.Clone(invocation.Want)
			return want
		}
		tb.Fatalf("missing scheduled invocation %s/%s", m.ID, export)
	}
	return want
}

func corpusCompletionLine(tb testing.TB, record corpusCompletion) string {
	tb.Helper()
	data, err := json.Marshal(record)
	if err != nil {
		tb.Fatal(err)
	}
	if len(data) > corpusCompletionLimit {
		tb.Fatal("corpus completion record limit")
	}
	return corpusChildMarker + " " + string(data)
}

func checkCorpusCompletion(output string, want corpusCompletion) error {
	var got corpusCompletion
	found := false
	for output != "" {
		line, rest, _ := strings.Cut(output, "\n")
		output = rest
		if !strings.HasPrefix(line, corpusChildMarker+" ") {
			continue
		}
		if found {
			return fmt.Errorf("duplicate completion")
		}
		found = true
		raw := strings.TrimPrefix(line, corpusChildMarker+" ")
		if len(raw) > corpusCompletionLimit {
			return fmt.Errorf("completion record limit")
		}
		dec := json.NewDecoder(strings.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&got); err != nil {
			return fmt.Errorf("completion JSON: %w", err)
		}
		var extra any
		if err := dec.Decode(&extra); err != io.EOF {
			return fmt.Errorf("completion trailing JSON")
		}
	}
	if !found {
		return fmt.Errorf("missing completion")
	}
	if got.Schema != 1 || got.ID != want.ID || got.Stage != want.Stage || got.Export != want.Export ||
		got.Init != want.Init || got.Bounds != want.Bounds || got.Target != want.Target ||
		got.GoVersion != want.GoVersion || got.Profile != want.Profile {
		return fmt.Errorf("completion identity differs")
	}
	if got.LoadedSHA256 != want.LoadedSHA256 {
		return fmt.Errorf("loaded artifact identity differs")
	}
	if got.Calls != want.Calls || got.InitCalls != want.InitCalls {
		return fmt.Errorf("invocation count differs")
	}
	if !slices.Equal(got.Args, want.Args) {
		return fmt.Errorf("invocation arguments differ")
	}
	if !slices.Equal(got.Results, want.Results) {
		return fmt.Errorf("raw results differ")
	}
	return nil
}

// Cap child output too: a broken child must not grow the parent's log without bound.
// Write still consumes the whole chunk, allowing the child to exit normally.
type corpusChildOutput struct {
	buffer   bytes.Buffer
	overflow bool
}

func (b *corpusChildOutput) Len() int       { return b.buffer.Len() }
func (b *corpusChildOutput) String() string { return b.buffer.String() }

const corpusChildOutputLimit = 1 << 20

func (b *corpusChildOutput) Write(p []byte) (int, error) {
	n := len(p)
	remaining := corpusChildOutputLimit - b.Len()
	if n > remaining {
		p = p[:remaining]
		b.overflow = true
	}
	_, _ = b.buffer.Write(p)
	return n, nil
}

func corpusIdentityFixture() corpusModule {
	raw := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 0, 1, 0x7f})),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0, 0x0b}))),
	)
	return corpusModule{ID: "identity-control", ArtifactSHA256: fmt.Sprintf("%x", sha256.Sum256(raw)), bytes: raw,
		Exec: []execEntry{{Export: "run", Want: []uint64{0}}}}
}

func TestCorpusCompletionWrappers(t *testing.T) {
	m := corpusIdentityFixture()
	want := corpusExpectedCompletion(t, m, "Exec", "run", "explicit")
	for _, mode := range []string{"correct", "omit", "substitute"} {
		t.Run(mode, func(t *testing.T) {
			loaded := m
			stage := "Exec"
			if mode == "substitute" {
				// Valid custom-section addition: a different module with the same interface/result.
				loaded.bytes = append(slices.Clone(m.bytes), wasmtest.Section(0, []byte{1, 'x'})...)
			}
			if mode == "omit" {
				stage = "CompileFull"
			}
			record := runCorpusStage(t, loaded, stage, "run", "explicit")
			if mode == "omit" {
				record.Stage = "Exec"
			} // mislabeled row does no invocation
			err := checkCorpusCompletion(corpusCompletionLine(t, record), want)
			switch mode {
			case "correct":
				if err != nil {
					t.Fatal(err)
				}
			case "omit":
				if err == nil || !strings.Contains(err.Error(), "invocation") {
					t.Fatalf("omitted zero-result call accepted or wrong reason: %v", err)
				}
			case "substitute":
				if !slices.Equal(record.Results, want.Results) {
					t.Fatal("substitute did not preserve result")
				}
				if err == nil || !strings.Contains(err.Error(), "loaded artifact") {
					t.Fatalf("same-result substitute accepted or wrong reason: %v", err)
				}
			}
		})
	}
}

func TestCorpusCompletionProtocol(t *testing.T) {
	m := corpusIdentityFixture()
	want := corpusExpectedCompletion(t, m, "Exec", "run", "explicit")
	valid := corpusCompletionLine(t, want)
	for _, tc := range []struct{ name, output, reason string }{
		{"missing", "PASS\n", "missing"},
		{"old-marker", corpusChildMarker + "\n", "missing"},
		{"embedded", "diagnostic: " + valid, "missing"},
		{"duplicate", valid + "\n" + valid, "duplicate"},
		{"malformed", corpusChildMarker + " {", "JSON"},
		{"unknown-field", strings.Replace(valid, "{", "{\"Unexpected\":1,", 1), "JSON"},
		{"trailing", valid + " {}", "trailing"},
		{"limit", corpusChildMarker + " " + strings.Repeat(" ", corpusCompletionLimit+1), "limit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := checkCorpusCompletion(tc.output, want); err == nil || !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("got %v, want %s", err, tc.reason)
			}
		})
	}
	if err := checkCorpusCompletion("=== RUN example\n"+valid+"\nPASS\n", want); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, reason string
		change       func(*corpusCompletion)
	}{
		{"schema", "identity", func(r *corpusCompletion) { r.Schema = 2 }},
		{"case", "identity", func(r *corpusCompletion) { r.ID = "other" }},
		{"stage", "identity", func(r *corpusCompletion) { r.Stage = "Instantiate" }},
		{"export", "identity", func(r *corpusCompletion) { r.Export = "other" }},
		{"bounds", "identity", func(r *corpusCompletion) { r.Bounds = "guard" }},
		{"target", "identity", func(r *corpusCompletion) { r.Target = "other" }},
		{"profile", "identity", func(r *corpusCompletion) { r.Profile = "unknown" }},
		{"repeat", "invocation", func(r *corpusCompletion) { r.Calls = 2 }},
		{"init", "invocation", func(r *corpusCompletion) { r.InitCalls = 1 }},
		{"args", "arguments", func(r *corpusCompletion) { r.Args = []uint64{1} }},
		{"result", "raw results", func(r *corpusCompletion) { r.Results = []uint64{1} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := want
			tc.change(&r)
			if err := checkCorpusCompletion(corpusCompletionLine(t, r), want); err == nil || !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("got %v, want %s", err, tc.reason)
			}
		})
	}
}

func TestCorpusChildOutputLimit(t *testing.T) {
	var b corpusChildOutput
	raw := make([]byte, corpusChildOutputLimit+17)
	for _, p := range [][]byte{raw[:7], raw[7:], raw[:1]} {
		if n, err := b.Write(p); n != len(p) || err != nil {
			t.Fatalf("write = %d, %v", n, err)
		}
	}
	var copied corpusChildOutput
	readerOnly := struct{ io.Reader }{bytes.NewReader(raw)}
	if n, err := io.Copy(&copied, readerOnly); err != nil || n != int64(len(raw)) {
		t.Fatalf("copy = %d, %v", n, err)
	}
	if !copied.overflow || copied.Len() != corpusChildOutputLimit {
		t.Fatalf("copy overflow=%t length=%d", copied.overflow, copied.Len())
	}
	if !b.overflow || b.Len() != corpusChildOutputLimit {
		t.Fatalf("overflow=%t length=%d", b.overflow, b.Len())
	}
}

// Separate input hashing and completion validation from compilation/guest cost.
// The marker row retains the old gate for paired observer-overhead measurements.
func BenchmarkCorpusCompletion(b *testing.B) {
	m := corpusIdentityFixture()
	want := corpusExpectedCompletion(b, m, "Exec", "run", "explicit")
	line := corpusCompletionLine(b, want)
	for _, enabled := range []bool{false, true} {
		b.Run(fmt.Sprintf("identity=%t", enabled), func(b *testing.B) {
			b.ReportAllocs()
			b.ReportMetric(float64(len(line)), "record-B")
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if enabled {
					if err := checkCorpusCompletion(line, want); err != nil {
						b.Fatal(err)
					}
				} else if !strings.Contains(line, corpusChildMarker) {
					b.Fatal("missing marker")
				}
			}
		})
	}
	for _, size := range []int{64, 64 << 10, 1 << 20} {
		raw := make([]byte, size)
		b.Run(fmt.Sprintf("load-hash/%d", size), func(b *testing.B) {
			b.SetBytes(int64(size))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				corpusHashSink = sha256.Sum256(raw)
			}
		})
	}
}

var corpusHashSink [32]byte

func TestCorpusCompletionDuplicateExports(t *testing.T) {
	m := corpusIdentityFixture()
	m.Artifact = "fixture.wasm"
	if err := validateCorpusModule(m); err != nil {
		t.Fatal(err)
	}
	m.Exec = append(m.Exec, execEntry{Export: "run", Args: []int32{7}, Want: []uint64{7}})
	if err := validateCorpusModule(m); err == nil || !strings.Contains(err.Error(), "duplicate direct export") {
		t.Fatalf("ambiguous schedule: %v", err)
	}
}

func TestCorpusCompletionExactSlots(t *testing.T) {
	m := corpusIdentityFixture()
	want := corpusExpectedCompletion(t, m, "Exec", "run", "explicit")
	// Retain uint64 precision, negative zero and NaN payload bits across JSON.
	want.Args = []uint64{0xffffffffffffffff, 0x8000000000000001}
	want.Results = []uint64{0xffffffffffffffff, 0x8000000000000000, 0x7ff0000000000001, 0x80000000, 0x7f800001}
	if err := checkCorpusCompletion(corpusCompletionLine(t, want), want); err != nil {
		t.Fatal(err)
	}
	for i := range want.Results {
		got := want
		got.Results = slices.Clone(want.Results)
		got.Results[i] ^= 1
		if err := checkCorpusCompletion(corpusCompletionLine(t, got), want); err == nil || !strings.Contains(err.Error(), "raw results") {
			t.Fatalf("slot %d: %v", i, err)
		}
	}
}
