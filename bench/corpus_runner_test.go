package wagobench

// This file deliberately uses a parent/child test process rather than
// t.Deadline. A bad native loop does not reliably yield to Go's test timeout,
// while a parent can always kill and reap the child that owns the JIT entry.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tetratelabs/wazero"
	wago "github.com/wago-org/wago"
	wasm "github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

const corpusChildMarker = "WAGO_CORPUS_OUTCOME="

// TestCorpus runs each declared pipeline stage and each runnable manifest
// export in a fresh OS process. WAGO_CORPUS_TIMEOUT controls the hard per-case
// deadline (default 15s). Build this package with wago_guardpage to include the
// guard-page cases; the ordinary build always covers explicit bounds and wazero.
func TestCorpus(t *testing.T) {
	if os.Getenv("WAGO_CORPUS_CHILD") == "1" {
		t.Skip("corpus child is selected directly by TestCorpusChild")
	}
	timeout := corpusTimeout(t)
	for mi, m := range loadCorpus(t) {
		if debugExport := os.Getenv("WAGO_CORPUS_DEBUG_EXPORT"); debugExport != "" {
			if m.name() != os.Getenv("WAGO_CORPUS_DEBUG_MODULE") {
				continue
			}
			t.Run(m.name()+"/Debug/"+debugExport, func(t *testing.T) {
				export := debugExport
				if os.Getenv("WAGO_CORPUS_DEBUG_FUNC") != "" {
					export = "__debug"
				}
				runCorpusChild(t, timeout, mi, "Debug", "wago", "explicit", export)
			})
			continue
		}
		if filter := os.Getenv("WAGO_CORPUS_FILTER"); filter != "" && filter != m.File && filter != m.name() {
			continue
		}
		for _, stage := range corpusStages(m) {
			if stage == "Exec" {
				if os.Getenv("WAGO_CORPUS_INIT_ONLY") == "1" && m.Init != "" {
					t.Run(m.name()+"/Init/"+m.Init, func(t *testing.T) {
						runCorpusChild(t, timeout, mi, "Init", "wago", "explicit", "")
					})
					continue
				}
				for _, ex := range m.Exec {
					t.Run(m.name()+"/"+stage+"/"+ex.Export, func(t *testing.T) {
						explicit := runCorpusChild(t, timeout, mi, stage, "wago", "explicit", ex.Export)
						if corpusGuardEnabled() {
							guard := runCorpusChild(t, timeout, mi, stage, "wago", "guard", ex.Export)
							if guard != explicit {
								t.Fatalf("explicit/guard outcome differs: explicit=%s guard=%s", explicit, guard)
							}
						}
						wazero := runCorpusChild(t, timeout, mi, stage, "wazero", "n/a", ex.Export)
						if wazero != explicit {
							t.Fatalf("wago/wazero outcome differs: wago=%s wazero=%s", explicit, wazero)
						}
					})
				}
				continue
			}
			t.Run(m.name()+"/"+stage, func(t *testing.T) {
				runCorpusChild(t, timeout, mi, stage, "wago", "explicit", "")
			})
		}
	}
}

// TestARM64WIPRegressions keeps the register-pressure failures which motivated
// the bulk-memory scratch reservation behind a process-level watchdog. A bad
// pinned-register assignment can turn fannkuch into native code which never
// reaches a Go safe point, so an in-process timeout is not sufficient here.
// The test is architecture-independent: it also checks the corpus artifacts and
// expected results on the other backends.
func TestARM64WIPRegressions(t *testing.T) {
	if os.Getenv("WAGO_CORPUS_CHILD") == "1" {
		t.Skip("corpus child is selected directly by TestCorpusChild")
	}
	const timeout = 5 * time.Second
	cases := []struct {
		module string
		export string
		want   string
	}{
		{"spectralnorm", "run", "result:[4bf31628]"},
		{"fannkuch", "run", "result:[16]"},
		{"sha256", "hashN", "result:[e409e0e7]"},
	}
	mods := loadCorpus(t)
	for _, tc := range cases {
		tc := tc
		t.Run(tc.module, func(t *testing.T) {
			mi := -1
			for i := range mods {
				if mods[i].name() == tc.module {
					mi = i
					break
				}
			}
			if mi < 0 {
				t.Fatalf("corpus module %q not found", tc.module)
			}
			for _, bounds := range []string{"explicit", "guard"} {
				if bounds == "guard" && !corpusGuardEnabled() {
					continue
				}
				t.Run(bounds, func(t *testing.T) {
					if got := runCorpusChild(t, timeout, mi, "Exec", "wago", bounds, tc.export); got != tc.want {
						t.Fatalf("%s.%s: got %s, want %s", tc.module, tc.export, got, tc.want)
					}
				})
			}
		})
	}
}

func corpusStages(m corpusModule) []string {
	if len(m.Stages) != 0 {
		return m.Stages
	}
	stages := []string{"Decode", "Validate", "Compile", "CompileFull", "Instantiate"}
	if len(m.Exec) != 0 {
		stages = append(stages, "Exec")
	}
	return stages
}

func corpusTimeout(t testing.TB) time.Duration {
	t.Helper()
	if s := os.Getenv("WAGO_CORPUS_TIMEOUT"); s != "" {
		d, err := time.ParseDuration(s)
		if err != nil || d <= 0 {
			t.Fatalf("invalid WAGO_CORPUS_TIMEOUT %q", s)
		}
		return d
	}
	return 15 * time.Second
}

func runCorpusChild(t *testing.T, timeout time.Duration, module int, stage, engine, bounds, export string) string {
	t.Helper()
	args := []string{"-test.run=^TestCorpusChild$", "-test.v"}
	if *includeISABenchmarks {
		args = append(args, "-wago.bench.isa")
	}
	cmd := exec.Command(os.Args[0], args...)
	cmd.Env = append(os.Environ(),
		"WAGO_CORPUS_CHILD=1",
		"WAGO_CORPUS_MODULE="+strconv.Itoa(module),
		"WAGO_CORPUS_STAGE="+stage,
		"WAGO_CORPUS_ENGINE="+engine,
		"WAGO_CORPUS_BOUNDS="+bounds,
		"WAGO_CORPUS_EXPORT="+export,
		"WAGO_CORPUS_ARG0="+os.Getenv("WAGO_CORPUS_ARG0"),
		"WAGO_CORPUS_SKIP_INIT="+os.Getenv("WAGO_CORPUS_SKIP_INIT"),
		"WAGO_CORPUS_DEBUG_ARGS="+os.Getenv("WAGO_CORPUS_DEBUG_ARGS"),
		"WAGO_CORPUS_DEBUG_FUNC="+os.Getenv("WAGO_CORPUS_DEBUG_FUNC"),
		"WAGO_CORPUS_DEBUG_CODE_FUNC="+os.Getenv("WAGO_CORPUS_DEBUG_CODE_FUNC"),
		"WAGO_CORPUS_DEBUG_SKIP_JSON_FREE="+os.Getenv("WAGO_CORPUS_DEBUG_SKIP_JSON_FREE"),
		"WAGO_CORPUS_DEBUG_REDUCED_LOOP="+os.Getenv("WAGO_CORPUS_DEBUG_REDUCED_LOOP"),
		"WAGO_CORPUS_DEBUG_REDUCED_OOB="+os.Getenv("WAGO_CORPUS_DEBUG_REDUCED_OOB"),
		"WAGO_CORPUS_DEBUG_SKIP_JSON_F23="+os.Getenv("WAGO_CORPUS_DEBUG_SKIP_JSON_F23"),
		"WAGO_CORPUS_DEBUG_SKIP_JSON_F41="+os.Getenv("WAGO_CORPUS_DEBUG_SKIP_JSON_F41"),
		"WAGO_CORPUS_DEBUG_SKIP_JSON_F10="+os.Getenv("WAGO_CORPUS_DEBUG_SKIP_JSON_F10"),
		"WAGO_CORPUS_DEBUG_SKIP_JSON_F22="+os.Getenv("WAGO_CORPUS_DEBUG_SKIP_JSON_F22"),
		"WAGO_CORPUS_DEBUG_SKIP_JSON_F8="+os.Getenv("WAGO_CORPUS_DEBUG_SKIP_JSON_F8"),
		"WAGO_CORPUS_DEBUG_SKIP_JSON_F7="+os.Getenv("WAGO_CORPUS_DEBUG_SKIP_JSON_F7"),
		"WAGO_CORPUS_DEBUG_SKIP_JSON_CALL16="+os.Getenv("WAGO_CORPUS_DEBUG_SKIP_JSON_CALL16"),
		"WAGO_CORPUS_DEBUG_GLOBAL="+os.Getenv("WAGO_CORPUS_DEBUG_GLOBAL"),
		"WAGO_CORPUS_DEBUG_MEM_HASH="+os.Getenv("WAGO_CORPUS_DEBUG_MEM_HASH"),
		"WAGO_CORPUS_DEBUG_MEM_RANGE="+os.Getenv("WAGO_CORPUS_DEBUG_MEM_RANGE"),
		"WAGO_CORPUS_DEBUG_MEM_HEX="+os.Getenv("WAGO_CORPUS_DEBUG_MEM_HEX"),
		"WAGO_CORPUS_DEBUG_BEFORE_GLOBAL="+os.Getenv("WAGO_CORPUS_DEBUG_BEFORE_GLOBAL"),
	)
	var b strings.Builder
	cmd.Stdout = &b
	cmd.Stderr = &b
	if err := cmd.Start(); err != nil {
		t.Fatalf("start corpus child: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("corpus child failed module=%d stage=%s engine=%s bounds=%s init/export=%s: %v\n%s", module, stage, engine, bounds, export, err, b.String())
		}
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		<-done // reap before reporting; never leave a stuck native child behind
		t.Fatalf("corpus timeout after %s: module=%d stage=%s engine=%s bounds=%s init/export=%s (optimization is process-fresh)\n%s", timeout, module, stage, engine, bounds, export, b.String())
	}
	for _, line := range strings.Split(b.String(), "\n") {
		if strings.HasPrefix(line, corpusChildMarker) {
			return strings.TrimPrefix(line, corpusChildMarker)
		}
	}
	t.Fatalf("corpus child produced no outcome module=%d stage=%s engine=%s bounds=%s export=%s\n%s", module, stage, engine, bounds, export, b.String())
	return ""
}

// TestCorpusChild is intentionally only useful when selected by its parent.
// It does one operation, prints a normalized outcome, and exits; every corpus
// case therefore gets fresh compiler package state as well as a fresh instance.
func TestCorpusChild(t *testing.T) {
	if os.Getenv("WAGO_CORPUS_CHILD") != "1" {
		t.Skip("parent-only helper")
	}
	mods := loadCorpus(t)
	mi, err := strconv.Atoi(os.Getenv("WAGO_CORPUS_MODULE"))
	if err != nil || mi < 0 || mi >= len(mods) {
		t.Fatalf("invalid WAGO_CORPUS_MODULE: %v", err)
	}
	m := mods[mi]
	if os.Getenv("WAGO_CORPUS_DEBUG_REDUCED_LOOP") == "1" {
		m.bytes = reducedCallLoadLoopModule()
		m.File = "reduced-call-load-loop.wasm"
	}
	if os.Getenv("WAGO_CORPUS_DEBUG_SKIP_JSON_F23") == "1" && m.name() == "json-as" {
		var err error
		m.bytes, err = replaceLocalCode(m.bytes, 22, []byte{0x00, 0x0b})
		if err != nil {
			t.Fatalf("replace json-as f23: %v", err)
		}
	}
	if os.Getenv("WAGO_CORPUS_DEBUG_SKIP_JSON_F41") == "1" && m.name() == "json-as" {
		var err error
		m.bytes, err = replaceLocalCode(m.bytes, 40, []byte{0x00, 0x0b})
		if err != nil {
			t.Fatalf("replace json-as f41: %v", err)
		}
	}
	if os.Getenv("WAGO_CORPUS_DEBUG_SKIP_JSON_F10") == "1" && m.name() == "json-as" {
		var err error
		m.bytes, err = replaceLocalCode(m.bytes, 9, []byte{0x00, 0x41, 0x00, 0x0b})
		if err != nil {
			t.Fatalf("replace json-as f10: %v", err)
		}
	}
	if os.Getenv("WAGO_CORPUS_DEBUG_SKIP_JSON_F22") == "1" && m.name() == "json-as" {
		var err error
		m.bytes, err = replaceLocalCode(m.bytes, 21, []byte{0x00, 0x0b})
		if err != nil {
			t.Fatalf("replace json-as f22: %v", err)
		}
	}
	if os.Getenv("WAGO_CORPUS_DEBUG_SKIP_JSON_F8") == "1" && m.name() == "json-as" {
		var err error
		m.bytes, err = replaceLocalCode(m.bytes, 7, []byte{0x00, 0x0b})
		if err != nil {
			t.Fatalf("replace json-as f8: %v", err)
		}
	}
	if os.Getenv("WAGO_CORPUS_DEBUG_SKIP_JSON_F7") == "1" && m.name() == "json-as" {
		var err error
		m.bytes, err = replaceLocalCode(m.bytes, 6, []byte{0x00, 0x0b})
		if err != nil {
			t.Fatalf("replace json-as f7: %v", err)
		}
	}
	if os.Getenv("WAGO_CORPUS_DEBUG_SKIP_JSON_FREE") == "1" && m.name() == "json-as" {
		needle := []byte{0x20, 0x00, 0x41, 0x14, 0x6a, 0x10, 0x10}
		replacement := []byte{0x20, 0x00, 0x41, 0x14, 0x6a, 0x1a, 0x01}
		if !bytes.Contains(m.bytes, needle) {
			t.Fatal("json-as f23 free-call sequence not found")
		}
		m.bytes = bytes.Replace(m.bytes, needle, replacement, 1)
	}
	if os.Getenv("WAGO_CORPUS_DEBUG_SKIP_JSON_CALL16") == "1" && m.name() == "json-as" {
		needle := []byte{0x20, 0x00, 0x41, 0x14, 0x6a, 0x10, 0x10}
		replacement := []byte{0x20, 0x00, 0x41, 0x14, 0x6a, 0x1a, 0x01}
		if bytes.Count(m.bytes, needle) < 2 {
			t.Fatal("json-as call16 sequences not found")
		}
		m.bytes = bytes.ReplaceAll(m.bytes, needle, replacement)
	}
	stage, engine, bounds := os.Getenv("WAGO_CORPUS_STAGE"), os.Getenv("WAGO_CORPUS_ENGINE"), os.Getenv("WAGO_CORPUS_BOUNDS")
	export := os.Getenv("WAGO_CORPUS_EXPORT")
	if raw := os.Getenv("WAGO_CORPUS_DEBUG_FUNC"); raw != "" {
		var indexes []uint32
		for _, part := range strings.Split(raw, ",") {
			idx, err := strconv.ParseUint(part, 10, 32)
			if err != nil {
				t.Fatalf("invalid WAGO_CORPUS_DEBUG_FUNC %q: %v", part, err)
			}
			indexes = append(indexes, uint32(idx))
		}
		m.bytes, err = addDebugFunctionExports(m.bytes, indexes)
		if err != nil {
			t.Fatalf("add debug export: %v", err)
		}
		export = fmt.Sprintf("__debug%d", len(indexes)-1)
	}
	if raw := os.Getenv("WAGO_CORPUS_DEBUG_GLOBAL"); raw != "" {
		idx, err := strconv.ParseUint(raw, 10, 32)
		if err != nil {
			t.Fatalf("invalid WAGO_CORPUS_DEBUG_GLOBAL %q: %v", raw, err)
		}
		m.bytes, err = addDebugGlobalExport(m.bytes, uint32(idx))
		if err != nil {
			t.Fatalf("add debug global export: %v", err)
		}
	}
	var outcome string
	switch engine {
	case "wago":
		outcome = corpusWagoChild(t, m, stage, bounds, export)
	case "wazero":
		outcome = corpusWazeroChild(t, m, stage, export)
	default:
		t.Fatalf("unknown engine %q", engine)
	}
	fmt.Println(corpusChildMarker + outcome)
}

func corpusWagoChild(t *testing.T, m corpusModule, stage, bounds, export string) string {
	t.Helper()
	if stage == "Decode" {
		if _, err := wasm.DecodeModule(m.bytes); err != nil {
			t.Fatal(err)
		}
		return "ok"
	}
	mod, err := wasm.DecodeModule(m.bytes)
	if err != nil {
		t.Fatal(err)
	}
	if stage == "Validate" {
		if err := wasm.ValidateModule(mod); err != nil {
			t.Fatal(err)
		}
		return "ok"
	}
	if err := wasm.ValidateModule(mod); err != nil {
		t.Fatal(err)
	}
	cfg := wago.NewRuntimeConfig().WithBoundsChecks(wago.BoundsChecksExplicit)
	if bounds == "guard" {
		cfg = cfg.WithBoundsChecks(wago.BoundsChecksSignalsBased)
	}
	c, err := wago.Compile(cfg, m.bytes)
	if err != nil {
		t.Fatal(err)
	}
	if stage == "Compile" || stage == "CompileFull" {
		return "ok"
	}
	in, err := wago.Instantiate(c, wago.InstantiateOptions{Imports: hostStubs(c)})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if os.Getenv("WAGO_CORPUS_DEBUG_REDUCED_LOOP") == "1" {
		next := []byte{200, 0, 0, 0}
		if os.Getenv("WAGO_CORPUS_DEBUG_REDUCED_OOB") == "1" {
			next = []byte{0, 0, 1, 0}
		}
		if !in.Write(104, next) {
			t.Fatal("initialize reduced loop next pointer")
		}
	}
	if os.Getenv("WAGO_CORPUS_DEBUG_MAP") == "1" {
		base, entries := in.CodeBase()
		fmt.Printf("WAGO_CORPUS_CODEBASE=%#x entries=%v\n", base, entries)
	}
	if raw := os.Getenv("WAGO_CORPUS_DEBUG_CODE_FUNC"); raw != "" {
		idx, err := strconv.Atoi(raw)
		if err != nil || idx < 0 || idx+1 >= len(c.Entry) {
			t.Fatalf("bad WAGO_CORPUS_DEBUG_CODE_FUNC %q", raw)
		}
		fmt.Printf("WAGO_CORPUS_CODE_FUNC=%d hex=%x\n", idx, c.Code[c.Entry[idx]:c.Entry[idx+1]])
		fmt.Printf("WAGO_CORPUS_CODE_BRANCHES=%s\n", arm64BranchTargets(c.Code, c.Entry[idx], c.Entry[idx+1]))
	}
	if stage == "Instantiate" {
		return "ok"
	}
	if m.Init != "" && os.Getenv("WAGO_CORPUS_SKIP_INIT") != "1" {
		if _, err := in.Invoke(m.Init); err != nil {
			t.Fatalf("init %s: %v", m.Init, err)
		}
	}
	if stage == "Init" {
		return "ok"
	}
	if stage == "Debug" {
		if raw := os.Getenv("WAGO_CORPUS_DEBUG_BEFORE_GLOBAL"); raw != "" {
			fn, err := strconv.Atoi(raw)
			if err != nil || fn < 0 {
				t.Fatalf("bad WAGO_CORPUS_DEBUG_BEFORE_GLOBAL %q", raw)
			}
			if _, err := in.Invoke(fmt.Sprintf("__debug%d", fn)); err != nil {
				t.Fatalf("debug pre-global invoke %d: %v", fn, err)
			}
		}
		if os.Getenv("WAGO_CORPUS_DEBUG_MEM_HASH") == "1" {
			off, size := debugMemoryRange(t)
			b, ok := in.Read(off, size)
			if !ok {
				t.Fatal("read debug memory")
			}
			return fmt.Sprintf("memory:%x", sha256.Sum256(b))
		}
		if os.Getenv("WAGO_CORPUS_DEBUG_MEM_HEX") == "1" {
			off, size := debugMemoryRange(t)
			b, ok := in.Read(off, size)
			if !ok {
				t.Fatal("read debug memory")
			}
			return fmt.Sprintf("memory:%x", b)
		}
		if os.Getenv("WAGO_CORPUS_DEBUG_GLOBAL") != "" {
			v, err := in.GlobalValue("__debugglobal")
			if err != nil {
				t.Fatalf("debug global: %v", err)
			}
			return fmt.Sprintf("global:%x", v.Bits())
		}
		var args []uint64
		for _, s := range strings.Split(os.Getenv("WAGO_CORPUS_DEBUG_ARGS"), ",") {
			if s == "" {
				continue
			}
			v, err := strconv.ParseUint(s, 0, 64)
			if err != nil {
				t.Fatalf("bad WAGO_CORPUS_DEBUG_ARGS %q: %v", s, err)
			}
			args = append(args, v)
		}
		if os.Getenv("WAGO_CORPUS_DEBUG_FUNC") == "" {
			r, err := in.Invoke(export, args...)
			if err != nil {
				t.Fatalf("debug invoke %s%v: %v", export, args, err)
			}
			return fmt.Sprintf("result:%x", r)
		}
		for i := 0; i < debugFunctionCount(); i++ {
			name := fmt.Sprintf("__debug%d", i)
			callArgs := args
			if i+1 != debugFunctionCount() {
				callArgs = nil
			}
			r, err := in.Invoke(name, callArgs...)
			if err != nil {
				t.Fatalf("debug invoke %s%v: %v", name, callArgs, err)
			}
			if i+1 == debugFunctionCount() {
				return fmt.Sprintf("result:%x", r)
			}
		}
		return "ok"
	}
	for _, ex := range m.Exec {
		if ex.Export != export {
			continue
		}
		args := make([]uint64, len(ex.Args))
		for i, a := range ex.Args {
			if i == 0 {
				if override, err := strconv.ParseInt(os.Getenv("WAGO_CORPUS_ARG0"), 10, 32); err == nil && os.Getenv("WAGO_CORPUS_ARG0") != "" {
					a = int32(override)
				}
			}
			args[i] = wago.I32(a)
		}
		r, err := in.Invoke(ex.Export, args...)
		if err != nil {
			t.Fatalf("invoke %s%v: %v", ex.Export, args, err)
		}
		return fmt.Sprintf("result:%x", r)
	}
	t.Fatalf("export %q not declared for %s", export, m.name())
	return ""
}

func arm64BranchTargets(code []byte, start, end int) string {
	var out []string
	for pc := start; pc+4 <= end; pc += 4 {
		w := uint32(code[pc]) | uint32(code[pc+1])<<8 | uint32(code[pc+2])<<16 | uint32(code[pc+3])<<24
		kind := ""
		bits := 0
		imm := int64(0)
		switch w & 0xfc000000 {
		case 0x14000000:
			kind, bits, imm = "b", 26, int64(w&0x03ffffff)
		case 0x94000000:
			kind, bits, imm = "bl", 26, int64(w&0x03ffffff)
		}
		if kind == "" && w&0xff000010 == 0x54000000 {
			kind, bits, imm = "b.cond", 19, int64((w>>5)&0x7ffff)
		}
		if kind == "" {
			continue
		}
		if imm&(int64(1)<<(bits-1)) != 0 {
			imm -= int64(1) << bits
		}
		out = append(out, fmt.Sprintf("%s:%x->%x", kind, pc-start, pc+int(imm*4)-start))
	}
	return strings.Join(out, ",")
}

func corpusWazeroChild(t *testing.T, m corpusModule, stage, export string) string {
	t.Helper()
	if stage != "Exec" && stage != "Debug" {
		t.Fatalf("wazero only participates in Exec, got %s", stage)
	}
	ctx := context.Background()
	r := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfigCompiler())
	defer r.Close(ctx)
	if _, err := r.NewHostModuleBuilder("env").NewFunctionBuilder().WithFunc(func(uint32, uint32, uint32, uint32) {}).Export("abort").Instantiate(ctx); err != nil {
		t.Fatal(err)
	}
	mod, err := r.Instantiate(ctx, m.bytes)
	if err != nil {
		t.Fatal(err)
	}
	if m.Init != "" && os.Getenv("WAGO_CORPUS_SKIP_INIT") != "1" {
		fn := mod.ExportedFunction(m.Init)
		if fn == nil {
			t.Fatalf("missing init %s", m.Init)
		}
		if _, err := fn.Call(ctx); err != nil {
			t.Fatalf("init %s: %v", m.Init, err)
		}
	}
	if stage == "Debug" && os.Getenv("WAGO_CORPUS_DEBUG_GLOBAL") != "" {
		if raw := os.Getenv("WAGO_CORPUS_DEBUG_BEFORE_GLOBAL"); raw != "" {
			fn, err := strconv.Atoi(raw)
			if err != nil || fn < 0 {
				t.Fatalf("bad WAGO_CORPUS_DEBUG_BEFORE_GLOBAL %q", raw)
			}
			f := mod.ExportedFunction(fmt.Sprintf("__debug%d", fn))
			if f == nil {
				t.Fatalf("missing debug export %d", fn)
			}
			if _, err := f.Call(ctx); err != nil {
				t.Fatalf("debug pre-global invoke %d: %v", fn, err)
			}
		}
		g := mod.ExportedGlobal("__debugglobal")
		if g == nil {
			t.Fatal("missing debug global")
		}
		return fmt.Sprintf("global:%x", g.Get())
	}
	if stage == "Debug" && os.Getenv("WAGO_CORPUS_DEBUG_MEM_HASH") == "1" {
		if raw := os.Getenv("WAGO_CORPUS_DEBUG_BEFORE_GLOBAL"); raw != "" {
			fn, err := strconv.Atoi(raw)
			if err != nil || fn < 0 {
				t.Fatalf("bad WAGO_CORPUS_DEBUG_BEFORE_GLOBAL %q", raw)
			}
			f := mod.ExportedFunction(fmt.Sprintf("__debug%d", fn))
			if f == nil {
				t.Fatalf("missing debug export %d", fn)
			}
			if _, err := f.Call(ctx); err != nil {
				t.Fatalf("debug pre-memory invoke %d: %v", fn, err)
			}
		}
		off, size := debugMemoryRange(t)
		b, ok := mod.Memory().Read(off, size)
		if !ok {
			t.Fatal("read debug memory")
		}
		return fmt.Sprintf("memory:%x", sha256.Sum256(b))
	}
	if stage == "Debug" && os.Getenv("WAGO_CORPUS_DEBUG_MEM_HEX") == "1" {
		if raw := os.Getenv("WAGO_CORPUS_DEBUG_BEFORE_GLOBAL"); raw != "" {
			fn, err := strconv.Atoi(raw)
			if err != nil || fn < 0 {
				t.Fatalf("bad WAGO_CORPUS_DEBUG_BEFORE_GLOBAL %q", raw)
			}
			f := mod.ExportedFunction(fmt.Sprintf("__debug%d", fn))
			if f == nil {
				t.Fatalf("missing debug export %d", fn)
			}
			if _, err := f.Call(ctx); err != nil {
				t.Fatalf("debug pre-memory invoke %d: %v", fn, err)
			}
		}
		off, size := debugMemoryRange(t)
		b, ok := mod.Memory().Read(off, size)
		if !ok {
			t.Fatal("read debug memory")
		}
		return fmt.Sprintf("memory:%x", b)
	}
	if stage == "Debug" {
		var args []uint64
		for _, s := range strings.Split(os.Getenv("WAGO_CORPUS_DEBUG_ARGS"), ",") {
			if s == "" {
				continue
			}
			v, err := strconv.ParseUint(s, 0, 64)
			if err != nil {
				t.Fatalf("bad WAGO_CORPUS_DEBUG_ARGS %q: %v", s, err)
			}
			args = append(args, v)
		}
		if os.Getenv("WAGO_CORPUS_DEBUG_FUNC") != "" {
			for i := 0; i < debugFunctionCount(); i++ {
				name := fmt.Sprintf("__debug%d", i)
				callArgs := args
				if i+1 != debugFunctionCount() {
					callArgs = nil
				}
				fn := mod.ExportedFunction(name)
				if fn == nil {
					t.Fatalf("missing debug export %s", name)
				}
				r, err := fn.Call(ctx, callArgs...)
				if err != nil {
					t.Fatalf("debug invoke %s%v: %v", name, callArgs, err)
				}
				if i+1 == debugFunctionCount() {
					return fmt.Sprintf("result:%x", r)
				}
			}
		}
		fn := mod.ExportedFunction(export)
		if fn == nil {
			t.Fatalf("missing debug export %s", export)
		}
		r, err := fn.Call(ctx, args...)
		if err != nil {
			t.Fatalf("debug invoke %s%v: %v", export, args, err)
		}
		return fmt.Sprintf("result:%x", r)
	}
	for _, ex := range m.Exec {
		if ex.Export != export {
			continue
		}
		args := make([]uint64, len(ex.Args))
		for i, a := range ex.Args {
			if i == 0 {
				if override, err := strconv.ParseInt(os.Getenv("WAGO_CORPUS_ARG0"), 10, 32); err == nil && os.Getenv("WAGO_CORPUS_ARG0") != "" {
					a = int32(override)
				}
			}
			args[i] = uint64(uint32(a))
		}
		fn := mod.ExportedFunction(ex.Export)
		if fn == nil {
			t.Fatalf("missing export %s", ex.Export)
		}
		r, err := fn.Call(ctx, args...)
		if err != nil {
			t.Fatalf("invoke %s%v: %v", ex.Export, args, err)
		}
		return fmt.Sprintf("result:%x", r)
	}
	t.Fatalf("export %q not declared for %s", export, m.name())
	return ""
}

func debugMemoryRange(t testing.TB) (uint32, uint32) {
	t.Helper()
	raw := os.Getenv("WAGO_CORPUS_DEBUG_MEM_RANGE")
	if raw == "" {
		return 0, 65536
	}
	parts := strings.Split(raw, ",")
	if len(parts) != 2 {
		t.Fatalf("bad WAGO_CORPUS_DEBUG_MEM_RANGE %q", raw)
	}
	off, err := strconv.ParseUint(parts[0], 0, 32)
	if err != nil {
		t.Fatal(err)
	}
	size, err := strconv.ParseUint(parts[1], 0, 32)
	if err != nil || off+size > 65536 {
		t.Fatalf("bad WAGO_CORPUS_DEBUG_MEM_RANGE %q", raw)
	}
	return uint32(off), uint32(size)
}

// addDebugFunctionExport adds a temporary function export to a wasm binary. It
// is used only by the child-process diagnosis path; the committed artifact is
// never modified.
func debugFunctionCount() int { return len(strings.Split(os.Getenv("WAGO_CORPUS_DEBUG_FUNC"), ",")) }

func addDebugFunctionExports(src []byte, functionIndexes []uint32) ([]byte, error) {
	if len(src) < 8 || string(src[:4]) != "\x00asm" {
		return nil, fmt.Errorf("not a wasm binary")
	}
	for off := 8; off < len(src); {
		sectionStart := off
		id := src[off]
		off++
		size, n, ok := readCorpusULEB(src[off:])
		if !ok || int(size) > len(src)-off-n {
			return nil, fmt.Errorf("malformed section at %d", sectionStart)
		}
		payloadAt := off + n
		end := payloadAt + int(size)
		if id != 7 {
			off = end
			continue
		}
		count, countN, ok := readCorpusULEB(src[payloadAt:end])
		if !ok {
			return nil, fmt.Errorf("malformed export count")
		}
		payload := append(corpusULEB(count+uint32(len(functionIndexes))), src[payloadAt+countN:end]...)
		for i, funcIndex := range functionIndexes {
			name := fmt.Sprintf("__debug%d", i)
			entry := append(corpusULEB(uint32(len(name))), name...)
			entry = append(entry, 0) // function export
			entry = append(entry, corpusULEB(funcIndex)...)
			payload = append(payload, entry...)
		}
		out := append([]byte{}, src[:sectionStart]...)
		out = append(out, 7)
		out = append(out, corpusULEB(uint32(len(payload)))...)
		out = append(out, payload...)
		return append(out, src[end:]...), nil
	}
	return nil, fmt.Errorf("no export section")
}

func addDebugGlobalExport(src []byte, globalIndex uint32) ([]byte, error) {
	if len(src) < 8 || string(src[:4]) != "\x00asm" {
		return nil, fmt.Errorf("not a wasm binary")
	}
	for off := 8; off < len(src); {
		sectionStart := off
		id := src[off]
		off++
		size, n, ok := readCorpusULEB(src[off:])
		if !ok || int(size) > len(src)-off-n {
			return nil, fmt.Errorf("malformed section at %d", sectionStart)
		}
		payloadAt := off + n
		end := payloadAt + int(size)
		if id != 7 {
			off = end
			continue
		}
		count, countN, ok := readCorpusULEB(src[payloadAt:end])
		if !ok {
			return nil, fmt.Errorf("malformed export count")
		}
		name := "__debugglobal"
		payload := append(corpusULEB(count+1), src[payloadAt+countN:end]...)
		entry := append(corpusULEB(uint32(len(name))), name...)
		entry = append(entry, 3) // global export
		entry = append(entry, corpusULEB(globalIndex)...)
		payload = append(payload, entry...)
		out := append([]byte{}, src[:sectionStart]...)
		out = append(out, 7)
		out = append(out, corpusULEB(uint32(len(payload)))...)
		out = append(out, payload...)
		return append(out, src[end:]...), nil
	}
	return nil, fmt.Errorf("no export section")
}

func replaceLocalCode(src []byte, localIndex int, body []byte) ([]byte, error) {
	if len(src) < 8 || string(src[:4]) != "\x00asm" {
		return nil, fmt.Errorf("not a wasm binary")
	}
	for off := 8; off < len(src); {
		sectionStart := off
		id := src[off]
		off++
		size, n, ok := readCorpusULEB(src[off:])
		if !ok || int(size) > len(src)-off-n {
			return nil, fmt.Errorf("malformed section at %d", sectionStart)
		}
		payloadAt, end := off+n, off+n+int(size)
		if id != 10 {
			off = end
			continue
		}
		count, countN, ok := readCorpusULEB(src[payloadAt:end])
		if !ok || localIndex < 0 || localIndex >= int(count) {
			return nil, fmt.Errorf("invalid code index %d", localIndex)
		}
		p := payloadAt + countN
		payload := append([]byte{}, src[payloadAt:p]...)
		for i := 0; i < int(count); i++ {
			bodySize, bodyN, ok := readCorpusULEB(src[p:end])
			if !ok || int(bodySize) > end-p-bodyN {
				return nil, fmt.Errorf("malformed code body %d", i)
			}
			bodyAt := p + bodyN
			bodyEnd := bodyAt + int(bodySize)
			if i == localIndex {
				payload = append(payload, corpusULEB(uint32(len(body)))...)
				payload = append(payload, body...)
			} else {
				payload = append(payload, src[p:bodyEnd]...)
			}
			p = bodyEnd
		}
		out := append([]byte{}, src[:sectionStart]...)
		out = append(out, 10)
		out = append(out, corpusULEB(uint32(len(payload)))...)
		out = append(out, payload...)
		return append(out, src[end:]...), nil
	}
	return nil, fmt.Errorf("no code section")
}

func readCorpusULEB(b []byte) (uint32, int, bool) {
	var v uint32
	for i, x := range b {
		if i == 5 {
			return 0, 0, false
		}
		v |= uint32(x&0x7f) << (7 * i)
		if x&0x80 == 0 {
			return v, i + 1, true
		}
	}
	return 0, 0, false
}

func corpusULEB(v uint32) []byte {
	var out []byte
	for {
		b := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			b |= 0x80
		}
		out = append(out, b)
		if v == 0 {
			return out
		}
	}
}

// reducedCallLoadLoopModule reproduces the json-as free-list loop shape: a call
// between a load and a next-pointer update, followed by br 1 to the loop header.
func reducedCallLoadLoopModule() []byte {
	run := []byte{
		0x02, 0x40, 0x03, 0x40, // block; loop
		0x20, 0x00, 0x20, 0x01, 0x47, 0x04, 0x40, // if local0 != local1
		0x20, 0x00, 0x28, 0x02, 0x04, 0x1a, // load [local0+4], drop
		0x20, 0x00, 0x41, 0x14, 0x6a, 0x10, 0x00, // helper(local0+20)
		0x20, 0x00, 0x28, 0x02, 0x04, 0x41, 0x7c, 0x71, 0x21, 0x00, // local0 = load & -4
		0x0c, 0x01, // br loop
		0x0b, 0x0b, 0x0b, 0x0b, // end if, loop, block, function
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}), wasmtest.Code(run))),
	)
}
