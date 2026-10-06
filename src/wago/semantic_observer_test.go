//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo && !wago_precompiled

package wago_test

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/wago"
	"github.com/wago-org/wago/tests/support/regressiontest"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Profiles select operation-specific predicates already used by the spec
// runner. Exact copies never receive an arithmetic-NaN or relaxed predicate.
const semanticCoreProfile = "core-exact-and-arithmetic-nan-v1"
const semanticRelaxedProfile = "relaxed-swizzle-v1"

type semanticCase struct {
	cmd     specExecCmd
	profile string
}

type semanticEvidence struct {
	CPUSelection                             uint32
	Features                                 string
	SourceSHA256, NativeSHA256, BinarySHA256 string
	Target, GoVersion, Bounds, HostABI       string
	RequiredCPU                              uint32
	Paths                                    []string
}

type semanticTerminal struct {
	cmd    specExecCmd
	status string
	raw    []uint64
	detail string
	trap   error
}

type semanticLedger struct {
	overflow  bool
	expected  map[string]string
	loaded    map[string]semanticEvidence
	scheduled []semanticCase
	terminal  []semanticTerminal
}

type semanticCounts struct{ Requested, Executed, Excluded, Missing int }

func semanticStatus(out specActionOutcome) string {
	if out.gap != specGapNone {
		return "unsupported"
	}
	err := out.harnessErr
	if err == nil {
		err = out.trap
	}
	if err == nil {
		return "return"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var resource *wago.ResourceLimitError
	var implementation *wago.ImplementationLimitError
	if errors.As(err, &resource) || errors.As(err, &implementation) {
		return "limit"
	}
	if out.harnessErr != nil {
		return "host-failure"
	}
	if out.trap != nil {
		var trap *wago.TrapError
		if errors.As(out.trap, &trap) {
			return "trap"
		}
		return "host-failure"
	}
	return "return"
}

func (l *semanticLedger) record(c specExecCmd, out specActionOutcome) {
	// The profile has at most 256 scalar/vector cases and two result slots each.
	// Retain one extra terminal to expose duplicates without unbounded growth.
	if len(l.terminal) >= 257 || len(out.results) > 2 {
		l.overflow = true
		return
	}
	detail := ""
	if out.harnessErr != nil || out.trap != nil {
		detail = fmt.Sprint(out.harnessErr, out.trap)
	}
	if len(detail) > 2048 {
		detail = detail[:2048]
	}
	// Invoke results are borrowed. Preserve their exact slots before any next call.
	l.terminal = append(l.terminal, semanticTerminal{c, semanticStatus(out), append([]uint64(nil), out.results...), detail, out.trap})
}

func (l *semanticLedger) check() (semanticCounts, error) {
	counts := semanticCounts{Requested: len(l.scheduled)}
	if l.overflow {
		return counts, fmt.Errorf("observer record limit")
	}
	if len(l.scheduled) == 0 || len(l.scheduled) > 256 || len(l.expected) == 0 || len(l.expected) > 16 {
		return counts, fmt.Errorf("invalid scheduled count")
	}
	byID := make(map[int]semanticTerminal, len(l.terminal))
	for _, r := range l.terminal {
		if _, exists := byID[r.cmd.Line]; exists {
			return counts, fmt.Errorf("duplicate terminal case %d", r.cmd.Line)
		}
		byID[r.cmd.Line] = r
		switch r.status {
		case "return", "trap":
			counts.Executed++
		case "rejected", "unsupported", "limit", "timeout", "host-failure", "mismatch":
			counts.Excluded++
		default:
			return counts, fmt.Errorf("unknown terminal status %q", r.status)
		}
	}
	var failures []string
	for name, want := range l.expected {
		got, ok := l.loaded[name]
		if !ok || got.SourceSHA256 != want || got.NativeSHA256 == "" {
			failures = append(failures, "loaded artifact identity: "+name)
		}
	}
	seen := make(map[int]bool, len(l.scheduled))
	for _, c := range l.scheduled {
		if seen[c.cmd.Line] {
			return counts, fmt.Errorf("duplicate scheduled case %d", c.cmd.Line)
		}
		seen[c.cmd.Line] = true
		if _, ok := l.expected[c.cmd.Action.Module]; !ok {
			failures = append(failures, "unscheduled artifact")
		}
		r, ok := byID[c.cmd.Line]
		if !ok {
			counts.Missing++
			failures = append(failures, fmt.Sprintf("missing terminal case %d", c.cmd.Line))
			continue
		}
		delete(byID, c.cmd.Line)
		if r.cmd.Action.Module != c.cmd.Action.Module || r.cmd.Action.Field != c.cmd.Action.Field {
			failures = append(failures, "wrong action identity")
			continue
		}
		if r.status != "return" && r.status != "trap" {
			failures = append(failures, "supported case did not execute: "+r.status)
			continue
		}
		if c.profile != semanticCoreProfile && c.profile != semanticRelaxedProfile {
			failures = append(failures, "unknown result profile")
			continue
		}
		if c.cmd.Type == "assert_trap" {
			if ok, _ := specTrapMatches(r.trap, c.cmd.Text); r.status != "trap" || !ok {
				failures = append(failures, "wrong trap")
			}
			continue
		}
		if r.status != "return" {
			failures = append(failures, "unexpected trap")
			continue
		}
		matched := false
		switch c.profile {
		case semanticCoreProfile:
			matched = len(c.cmd.Either) == 0 && matchSpecResults(r.raw, c.cmd.Expected, specModule{})
		case semanticRelaxedProfile:
			matched = len(c.cmd.Expected) == 0 && matchEitherResult(specModule{}, r.raw, c.cmd.Either)
		}
		if !matched {
			failures = append(failures, fmt.Sprintf("result mismatch: case %d raw=%x", c.cmd.Line, r.raw))
		}
	}
	if len(byID) != 0 {
		failures = append(failures, "unscheduled terminal case")
	}
	if counts.Executed == 0 {
		failures = append(failures, "no supported execution")
	}
	if len(failures) != 0 {
		return counts, errors.New(strings.Join(failures, "; "))
	}
	return counts, nil
}

func semanticScalar(typ string, bits uint64) specValue {
	raw, _ := json.Marshal(fmt.Sprint(bits))
	return specValue{Type: typ, Value: raw}
}
func semanticNaN(typ, class string) specValue {
	raw, _ := json.Marshal("nan:" + class)
	return specValue{Type: typ, Value: raw}
}
func semanticVector(lanes []byte) specValue {
	s := make([]string, len(lanes))
	for i, lane := range lanes {
		s[i] = fmt.Sprint(lane)
	}
	raw, _ := json.Marshal(s)
	return specValue{Type: "v128", LaneType: "i8", Value: raw}
}

func semanticFixtures() (map[string][]byte, []semanticCase, specExecFile) {
	type function struct {
		name            string
		params, results []wasm.ValType
		body            []byte
		args, want      []specValue
		trap            bool
	}
	f32 := func(bits uint32) []byte { return binary.LittleEndian.AppendUint32([]byte{0x43}, bits) }
	f64 := func(bits uint64) []byte { return binary.LittleEndian.AppendUint64([]byte{0x44}, bits) }
	vec := []byte{1, 3, 5, 7, 9, 11, 13, 15, 17, 19, 21, 23, 25, 27, 29, 31}
	funcs := []function{
		{name: "width32", results: []wasm.ValType{wasm.I32}, body: []byte{0x41, 0x7f}, want: []specValue{semanticScalar("i32", 0xffffffff)}},
		{name: "width64", results: []wasm.ValType{wasm.I64}, body: append([]byte{0x42}, wasmtest.SLEB64(0x100000001)...), want: []specValue{semanticScalar("i64", 0x100000001)}},
		{name: "negative-zero32", results: []wasm.ValType{wasm.F32}, body: f32(0x80000000), want: []specValue{semanticScalar("f32", 0x80000000)}},
		{name: "negative-zero64", results: []wasm.ValType{wasm.F64}, body: f64(0x8000000000000000), want: []specValue{semanticScalar("f64", 0x8000000000000000)}},
		{name: "nan-copy32", params: []wasm.ValType{wasm.F32}, results: []wasm.ValType{wasm.F32}, body: []byte{0x20, 0}, args: []specValue{semanticScalar("f32", 0x7fa12345)}, want: []specValue{semanticScalar("f32", 0x7fa12345)}},
		{name: "nan-copy64", params: []wasm.ValType{wasm.F64}, results: []wasm.ValType{wasm.F64}, body: []byte{0x20, 0}, args: []specValue{semanticScalar("f64", 0xfff123456789abcd)}, want: []specValue{semanticScalar("f64", 0xfff123456789abcd)}},
		{name: "lane-order", results: []wasm.ValType{wasm.V128}, body: append([]byte{0xfd, 0x0c}, vec...), want: []specValue{semanticVector(vec)}},
		{name: "nan-arithmetic32", params: []wasm.ValType{wasm.F32}, results: []wasm.ValType{wasm.F32}, body: append(append([]byte{0x20, 0}, f32(0)...), 0x92), args: []specValue{semanticScalar("f32", 0x7fc12345)}, want: []specValue{semanticNaN("f32", "arithmetic")}},
		{name: "nan-arithmetic64", params: []wasm.ValType{wasm.F64}, results: []wasm.ValType{wasm.F64}, body: append(append([]byte{0x20, 0}, f64(0)...), 0xa0), args: []specValue{semanticScalar("f64", 0x7ff8123456789abc)}, want: []specValue{semanticNaN("f64", "arithmetic")}},
		{name: "nan-canonical", results: []wasm.ValType{wasm.F64}, body: append(append(f64(0), f64(0)...), 0xa3), want: []specValue{semanticNaN("f64", "canonical")}},
		{name: "unreachable", body: []byte{0}, trap: true},
	}
	var types, signatures, bodies, exports [][]byte
	cases := make([]semanticCase, 0, len(funcs)+1)
	sf := specExecFile{Commands: []specExecCmd{{Type: "module", Line: 0, Name: "core", Filename: "core.wasm"}}}
	for i, f := range funcs {
		types = append(types, wasmtest.FuncType(f.params, f.results))
		signatures = append(signatures, wasmtest.ULEB(uint32(i)))
		body := append(append([]byte{0}, f.body...), 0x0b)
		bodies = append(bodies, append(wasmtest.ULEB(uint32(len(body))), body...))
		exports = append(exports, wasmtest.ExportEntry(f.name, 0, uint32(i)))
		c := specExecCmd{Type: "assert_return", Line: i + 1, Action: specAction{Type: "invoke", Module: "core", Field: f.name, Args: f.args}, Expected: f.want}
		if f.trap {
			c.Type = "assert_trap"
			c.Text = "unreachable"
		}
		cases = append(cases, semanticCase{c, semanticCoreProfile})
		sf.Commands = append(sf.Commands, c)
	}
	core := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(3, wasmtest.Vec(signatures...)), wasmtest.Section(7, wasmtest.Vec(exports...)), wasmtest.Section(10, wasmtest.Vec(bodies...)))
	// Use the pinned Core 3 relaxed-swizzle case for indices 16..31. Its two
	// permitted complete vectors differ across PSHUFB and TBL targets.
	body := append([]byte{0, 0xfd, 0x0c}, vec...)
	body = append(body, 0xfd, 0x0c)
	for i := byte(16); i < 32; i++ {
		body = append(body, i)
	}
	body = append(body, 0xfd, 0x80, 0x02, 0x0b)
	relaxed := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.V128}))), wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("swizzle", 0, 0))), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
	c := specExecCmd{Type: "assert_return", Line: len(funcs) + 2, Action: specAction{Type: "invoke", Module: "relaxed", Field: "swizzle"}, Either: []specValue{semanticVector(make([]byte, 16)), semanticVector(vec)}}
	cases = append(cases, semanticCase{c, semanticRelaxedProfile})
	sf.Commands = append(sf.Commands, specExecCmd{Type: "module", Line: len(funcs) + 1, Name: "relaxed", Filename: "relaxed.wasm"}, c)
	return map[string][]byte{"core": core, "relaxed": relaxed}, cases, sf
}

func semanticBinaryHash(t testing.TB) string {
	t.Helper()
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func TestSemanticProfilesAndExecutionIdentity(t *testing.T) {
	if regressiontest.RunIsolated(t, 30*time.Second) {
		return
	}
	modules, cases, sf := semanticFixtures()
	dir := t.TempDir()
	binaryHash := semanticBinaryHash(t)
	for name, raw := range modules {
		if err := os.WriteFile(filepath.Join(dir, name+".wasm"), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, control := range []string{"correct", "omit-supported", "substitute-valid", "reject-supported"} {
		t.Run(control, func(t *testing.T) {
			ledger := semanticLedger{expected: map[string]string{}, loaded: map[string]semanticEvidence{}, scheduled: cases}
			for name, raw := range modules {
				ledger.expected[name] = fmt.Sprintf("%x", sha256.Sum256(raw))
			}
			observer := &specExecutionObserver{action: ledger.record}
			observer.rejected = func(c specExecCmd, err error) {
				for _, scheduled := range cases {
					if scheduled.cmd.Action.Module == c.Name {
						ledger.terminal = append(ledger.terminal, semanticTerminal{cmd: scheduled.cmd, status: "rejected", detail: err.Error()})
					}
				}
			}
			if control == "reject-supported" {
				observer.load = func(c specExecCmd, raw []byte) []byte {
					if c.Name == "core" {
						return []byte("invalid Wasm")
					}
					return raw
				}
			}

			observer.loaded = func(c specExecCmd, raw []byte, m specModule) {
				native, cpu, paths := wago.SemanticCodeEvidenceForTest(t, raw, m.compiled, m.inst)
				features := "core-simd"
				if c.Name == "relaxed" {
					features += ",relaxed-simd"
				}
				ledger.loaded[c.Name] = semanticEvidence{CPUSelection: wago.SemanticCPUProfileForTest(), Features: features, SourceSHA256: fmt.Sprintf("%x", sha256.Sum256(raw)), NativeSHA256: native, BinarySHA256: binaryHash, Target: runtime.GOOS + "/" + runtime.GOARCH, GoVersion: runtime.Version(), Bounds: "explicit", HostABI: "Instance.Invoke", RequiredCPU: cpu, Paths: paths}
			}
			if control == "omit-supported" {
				observer.omit = func(c specExecCmd) bool { return c.Line == 3 }
			}
			if control == "substitute-valid" {
				observer.load = func(c specExecCmd, raw []byte) []byte {
					if c.Name != "core" {
						return raw
					}
					return append(append([]byte(nil), raw...), wasmtest.Section(0, wasmtest.Name("valid-substitute"))...)
				}
			}
			cfg := wago.NewRuntimeConfig().WithBoundsChecks(wago.BoundsChecksExplicit).WithFunctionWorkers(1)
			stats := runSpecExecFileObserved(t, "semantic-profile", dir, sf, cfg, nil, observer)
			// Both faulty wrappers still pass the runner's individual value assertions.
			// The independent scheduled-case/loaded-identity gate must reject them.
			wantModules, wantSkipped := 2, 0
			if control == "reject-supported" {
				wantModules, wantSkipped = 1, len(cases)-1
			}
			if stats.modulesPassed != wantModules || stats.modulesFailed != 0 || stats.assertionsFailed != 0 || stats.assertionsSkipped != wantSkipped {
				t.Fatalf("unexpected runner stats: %+v", stats)
			}
			counts, err := ledger.check()
			t.Logf("counts=%+v profiles=%s,%s evidence=%+v", counts, semanticCoreProfile, semanticRelaxedProfile, ledger.loaded)
			for _, r := range ledger.terminal {
				t.Logf("case=%d module=%s export=%s terminal=%s raw=%x error=%s", r.cmd.Line, r.cmd.Action.Module, r.cmd.Action.Field, r.status, r.raw, r.detail)
			}
			wantReason := ""
			if control == "omit-supported" {
				wantReason = "missing terminal"
			}
			if control == "substitute-valid" {
				wantReason = "loaded artifact identity"
			}
			if control == "reject-supported" {
				wantReason = "supported case did not execute"
				if counts.Excluded != len(cases)-1 || counts.Executed != 1 || counts.Missing != 0 {
					t.Fatalf("rejection accounting: %+v", counts)
				}
			}
			if wantReason == "" {
				if err != nil {
					t.Fatal(err)
				}
				if counts.Executed != len(cases) || counts.Excluded != 0 || counts.Missing != 0 {
					t.Fatalf("incomplete supported execution: %+v", counts)
				}
			} else if err == nil || !strings.Contains(err.Error(), wantReason) {
				t.Fatalf("control %s: %v, want %s", control, err, wantReason)
			}
			if control == "correct" {
				last := ledger.terminal[len(ledger.terminal)-1]
				want := cases[len(cases)-1].cmd.Either[0]
				if runtime.GOARCH == "amd64" {
					want = cases[len(cases)-1].cmd.Either[1]
				}
				if !matchResult(last.raw, want) {
					t.Fatalf("Wago target choice: %x", last.raw)
				}
			}
		})
	}
}

func TestSemanticProfileObserverControls(t *testing.T) {
	_, cases, _ := semanticFixtures()
	for _, tc := range []struct {
		name  string
		index int
		wrong []uint64
	}{
		{"i32-width", 0, []uint64{0xffff}}, {"i64-width", 1, []uint64{1}},
		{"signed-zero32", 2, []uint64{0}}, {"signed-zero64", 3, []uint64{0}},
		{"nan-copy32", 4, []uint64{0x7fc00000}}, {"nan-copy64", 5, []uint64{0x7ff8000000000000}},
		{"lane-order", 6, []uint64{0x1f1d1b1917151311, 0x0f0d0b0907050301}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if matchSpecResults(tc.wrong, cases[tc.index].cmd.Expected, specModule{}) {
				t.Fatal("wrong bits accepted")
			}
		})
	}
	for _, tc := range []struct {
		typ                                     string
		canonical, arithmetic, signal, infinity uint64
	}{
		{"f32", 0xffc00000, 0x7fc12345, 0x7fa12345, 0x7f800000},
		{"f64", 0xfff8000000000000, 0x7ff8123456789abc, 0x7ff123456789abcd, 0x7ff0000000000000},
	} {
		if !matchResult([]uint64{tc.canonical}, semanticNaN(tc.typ, "canonical")) || !matchResult([]uint64{tc.arithmetic}, semanticNaN(tc.typ, "arithmetic")) {
			t.Fatal("permitted NaN rejected")
		}
		if matchResult([]uint64{tc.arithmetic}, semanticNaN(tc.typ, "canonical")) || matchResult([]uint64{tc.signal}, semanticNaN(tc.typ, "arithmetic")) || matchResult([]uint64{tc.infinity}, semanticNaN(tc.typ, "arithmetic")) {
			t.Fatal("invalid NaN class accepted")
		}
	}
	alt := cases[len(cases)-1].cmd.Either
	if !matchEitherResult(specModule{}, []uint64{0, 0}, alt) || !matchEitherResult(specModule{}, []uint64{0x0f0d0b0907050301, 0x1f1d1b1917151311}, alt) || matchEitherResult(specModule{}, []uint64{1, 0}, alt) {
		t.Fatal("relaxed swizzle predicate is wrong")
	}
	// Undefined high ABI-slot bits do not change an i32's 32 semantic bits.
	if !matchResult([]uint64{0xdeadbeefffffffff}, cases[0].cmd.Expected[0]) {
		t.Fatal("i32 high slot bits constrained")
	}
}

func TestSemanticTerminalStatusGate(t *testing.T) {
	modules, cases, _ := semanticFixtures()
	for _, status := range []string{"rejected", "unsupported", "limit", "timeout", "host-failure", "mismatch"} {
		t.Run(status, func(t *testing.T) {
			source := fmt.Sprintf("%x", sha256.Sum256(modules["core"]))
			l := semanticLedger{expected: map[string]string{"core": source}, loaded: map[string]semanticEvidence{"core": {SourceSHA256: source, NativeSHA256: "test-only"}}, scheduled: cases[:1], terminal: []semanticTerminal{{cmd: cases[0].cmd, status: status}}}
			counts, err := l.check()
			if err == nil || counts.Executed != 0 || counts.Excluded != 1 {
				t.Fatalf("%s counted as semantic success: %+v %v", status, counts, err)
			}
		})
	}
	for _, tc := range []struct {
		name string
		out  specActionOutcome
		want string
	}{
		{"return", specActionOutcome{results: []uint64{1}}, "return"},
		{"trap", specActionOutcome{trap: &wago.TrapError{Code: wago.TrapUnreachable}}, "trap"},
		{"unsupported", specActionOutcome{gap: specGapAbsentExport}, "unsupported"},
		{"timeout", specActionOutcome{trap: context.DeadlineExceeded}, "timeout"},
		{"limit", specActionOutcome{trap: &wago.ResourceLimitError{}}, "limit"},
		{"host-failure", specActionOutcome{harnessErr: errors.New("host failure")}, "host-failure"},
		{"non-trap-error", specActionOutcome{trap: errors.New("invoke failure")}, "host-failure"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := semanticStatus(tc.out); got != tc.want {
				t.Fatalf("status=%s want %s", got, tc.want)
			}
		})
	}
}

func BenchmarkSemanticResultObservation(b *testing.B) {
	modules, cases, _ := semanticFixtures()
	cfg := wago.NewRuntimeConfig().WithBoundsChecks(wago.BoundsChecksExplicit).WithFunctionWorkers(1)
	compiled, err := wago.Compile(cfg, modules["core"])
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	inst, err := wago.Instantiate(compiled, nil)
	if err != nil {
		b.Fatal(err)
	}
	defer inst.Close()
	for _, enabled := range []bool{false, true} {
		b.Run(fmt.Sprintf("observer=%t", enabled), func(b *testing.B) {
			m := specModule{inst: inst, compiled: compiled}
			var ledger semanticLedger
			ledger.terminal = make([]semanticTerminal, 0, 1)
			if enabled {
				m.observer = &specExecutionObserver{action: ledger.record}
			}
			cmd := cases[5].cmd // Preserve the complete noncanonical f64 NaN payload.
			out := invokeAction(cmd, m, nil)
			if out.trap != nil || !matchSpecResults(out.results, cmd.Expected, m) {
				b.Fatal("baseline semantic check failed")
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				ledger.terminal = ledger.terminal[:0]
				out = invokeAction(cmd, m, nil)
			}
			b.StopTimer()
			if out.trap != nil || !matchSpecResults(out.results, cmd.Expected, m) {
				b.Fatal("final semantic check failed")
			}
			if enabled && (len(ledger.terminal) != 1 || !matchSpecResults(ledger.terminal[0].raw, cmd.Expected, m)) {
				b.Fatal("missing observed result")
			}
		})
	}
}
