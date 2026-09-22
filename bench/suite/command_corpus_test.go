package wagobench

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	wazerowasi "github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	wazerosys "github.com/tetratelabs/wazero/sys"
	"github.com/wago-org/wago"
)

type commandOutput struct {
	results        []uint64
	stdout, stderr []byte
	files          map[string][]byte
}

func TestCommandArgsMulticall(t *testing.T) {
	m := corpusModule{ID: "coreutils-sort", Command: &commandEntry{Argv0: "coreutils", Args: []string{"sort", "/records.txt"}}}
	if got := commandArgs(m); !slices.Equal(got, []string{"coreutils", "sort", "/records.txt"}) {
		t.Fatalf("multicall arguments = %q", got)
	}
	m.Command.Argv0 = ""
	if got := commandArgs(m); !slices.Equal(got, []string{"coreutils-sort", "sort", "/records.txt"}) {
		t.Fatalf("default arguments = %q", got)
	}
}

func TestCommandTreeSHA256(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "data.json")
	if err := os.WriteFile(path, []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := commandTreeSHA256(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("second"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := commandTreeSHA256(root)
	if err != nil || first == second {
		t.Fatalf("tree digest did not change with file bytes: %s, %s, %v", first, second, err)
	}
	if err := os.Rename(path, filepath.Join(root, "renamed.json")); err != nil {
		t.Fatal(err)
	}
	third, err := commandTreeSHA256(root)
	if err != nil || second == third {
		t.Fatalf("tree digest did not change with file name: %s, %s, %v", second, third, err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Symlink(filepath.Join(root, "renamed.json"), path); err != nil {
			t.Fatal(err)
		}
		if _, err := commandTreeSHA256(root); err == nil {
			t.Fatal("tree digest accepted a symlink")
		}
	}
}

func TestCommandSupportsPlatform(t *testing.T) {
	all := corpusModule{Command: &commandEntry{}}
	if !commandSupportsPlatform(all, "linux", "amd64") {
		t.Fatal("empty platform list should support every platform")
	}
	darwinARM64 := corpusModule{Command: &commandEntry{Platforms: []string{"darwin/arm64"}}}
	if !commandSupportsPlatform(darwinARM64, "darwin", "arm64") {
		t.Fatal("darwin/arm64 should be supported")
	}
	if commandSupportsPlatform(darwinARM64, "linux", "amd64") {
		t.Fatal("linux/amd64 should not be supported")
	}
}

func TestCommandScratchPreopen(t *testing.T) {
	m := corpusModule{Command: &commandEntry{
		Preopen: "workloads/applications/ecpbram/inputs",
		Inputs:  map[string]string{"README.txt": "ea6f178748399f25f5890284b55d4ea6e6ba8872e277b6c1e5e6b4849f4b9a98"},
		Outputs: map[string]string{"bram.hex": "unused"},
	}}
	dir, cleanup, err := commandScratchPreopen(m)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if dir == commandPreopen(m) {
		t.Fatal("generated outputs must use an isolated preopen")
	}
	if _, err := os.ReadFile(filepath.Join(dir, "README.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bram.hex"), []byte("generated"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := commandOutputFiles(m, dir)
	if err != nil || string(files["bram.hex"]) != "generated" {
		t.Fatalf("output files = %q, %v", files, err)
	}
	if _, err := os.Stat(filepath.Join(commandPreopen(m), "bram.hex")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("committed preopen was modified: %v", err)
	}
	for _, path := range []string{"", "../escape", "/absolute"} {
		if validCommandPath(path) {
			t.Fatalf("unsafe command path %q accepted", path)
		}
	}
	if runtime.GOOS != "windows" {
		outside := filepath.Join(t.TempDir(), "outside")
		if err := os.WriteFile(outside, []byte("private"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(dir, "bram.hex.link")); err != nil {
			t.Fatal(err)
		}
		m.Command.Outputs = map[string]string{"bram.hex.link": "unused"}
		if _, err := commandOutputFiles(m, dir); err == nil {
			t.Fatal("output symlink escaped its preopen")
		}
	}
}

func commandCorpus(tb testing.TB) []corpusModule {
	tb.Helper()
	var out []corpusModule
	for _, m := range loadCorpus(tb) {
		if m.Command != nil && m.supports("CommandExec") && commandSupportsPlatform(m, runtime.GOOS, runtime.GOARCH) {
			validateCommandInputs(tb, m)
			out = append(out, m)
		}
	}
	return out
}

func validateCommandInputs(tb testing.TB, m corpusModule) {
	tb.Helper()
	if m.Command.ReadOnlyPreopen != "" {
		if !validCommandPath(m.Command.ReadOnlyPreopen) {
			tb.Fatalf("%s invalid read-only preopen %q", m.ID, m.Command.ReadOnlyPreopen)
		}
		root := filepath.Join(corpusDir, m.Command.ReadOnlyPreopen)
		got, err := commandTreeSHA256(root)
		if err != nil || got != m.Command.ReadOnlyTreeSHA256 {
			tb.Fatalf("%s read-only tree sha256 = %s, want %s (error=%v)", m.ID, got, m.Command.ReadOnlyTreeSHA256, err)
		}
	} else if m.Command.ReadOnlyTreeSHA256 != "" {
		tb.Fatalf("%s declares a tree digest without a read-only preopen", m.ID)
	}
	if m.Command.Preopen == "" {
		if len(m.Command.Inputs) != 0 || len(m.Command.Outputs) != 0 {
			tb.Fatalf("%s declares files without a preopen directory", m.ID)
		}
		return
	}
	for rel, want := range m.Command.Outputs {
		if !validCommandPath(rel) || len(want) != 64 {
			tb.Fatalf("%s invalid output path or digest %q", m.ID, rel)
		}
		if _, input := m.Command.Inputs[rel]; input {
			tb.Fatalf("%s output %q overlaps an input", m.ID, rel)
		}
	}
	root := commandPreopen(m)
	seen := make(map[string]bool, len(m.Command.Inputs))
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		want, ok := m.Command.Inputs[rel]
		if !ok {
			return fmt.Errorf("undeclared input file %s", rel)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if got := fmt.Sprintf("%x", sha256.Sum256(data)); got != want {
			return fmt.Errorf("input %s sha256 = %s, want %s", rel, got, want)
		}
		seen[rel] = true
		return nil
	})
	if err != nil {
		tb.Fatalf("%s inputs: %v", m.ID, err)
	}
	for rel := range m.Command.Inputs {
		if !seen[rel] {
			tb.Fatalf("%s declared input %s is missing", m.ID, rel)
		}
	}
}

// commandTreeSHA256 covers both file names and bytes, rejecting symlinks and
// other special files so an immutable device database can be pinned compactly.
func commandTreeSHA256(root string) (string, error) {
	var entries []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("non-regular command input %s", path)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		entries = append(entries, fmt.Sprintf("%s\x00%x\n", filepath.ToSlash(rel), sha256.Sum256(data)))
		return nil
	})
	if err != nil {
		return "", err
	}
	slices.Sort(entries)
	h := sha256.New()
	for _, entry := range entries {
		_, _ = io.WriteString(h, entry)
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func commandSupportsPlatform(m corpusModule, goos, goarch string) bool {
	if len(m.Command.Platforms) == 0 {
		return true
	}
	return slices.Contains(m.Command.Platforms, goos+"/"+goarch)
}

func commandInput(tb testing.TB, m corpusModule) []byte {
	tb.Helper()
	if m.Command.Stdin == "" {
		return nil
	}
	path := filepath.Join(commandPreopen(m), filepath.FromSlash(m.Command.Stdin))
	b, err := os.ReadFile(path)
	if err != nil {
		tb.Fatalf("read command input %s: %v", m.Command.Stdin, err)
	}
	return b
}

func commandPreopen(m corpusModule) string {
	if m.Command.Preopen == "" {
		return ""
	}
	return filepath.Join(corpusDir, m.Command.Preopen)
}

func validCommandPath(rel string) bool {
	return rel != "" && rel == filepath.ToSlash(filepath.FromSlash(rel)) && filepath.IsLocal(filepath.FromSlash(rel))
}

// File-producing commands get an isolated writable preopen. The committed
// inputs are never modified by tests or timed benchmark iterations.
func commandScratchPreopen(m corpusModule) (string, func(), error) {
	root := commandPreopen(m)
	if len(m.Command.Outputs) == 0 {
		return root, func() {}, nil
	}
	dir, err := os.MkdirTemp("", "wago-command-corpus-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	for rel := range m.Command.Inputs {
		if !validCommandPath(rel) {
			cleanup()
			return "", nil, fmt.Errorf("invalid input path %q", rel)
		}
		path := filepath.FromSlash(rel)
		data, err := os.ReadFile(filepath.Join(root, path))
		if err == nil {
			err = os.MkdirAll(filepath.Dir(filepath.Join(dir, path)), 0o755)
		}
		if err == nil {
			err = os.WriteFile(filepath.Join(dir, path), data, 0o644)
		}
		if err != nil {
			cleanup()
			return "", nil, err
		}
	}
	return dir, cleanup, nil
}

func commandOutputFiles(m corpusModule, dir string) (map[string][]byte, error) {
	if len(m.Command.Outputs) == 0 {
		return nil, nil
	}
	resolvedRoot, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, err
	}
	files := make(map[string][]byte, len(m.Command.Outputs))
	for rel := range m.Command.Outputs {
		resolved, err := filepath.EvalSymlinks(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return nil, fmt.Errorf("output %q: %w", rel, err)
		}
		inside, err := filepath.Rel(resolvedRoot, resolved)
		if err != nil || !filepath.IsLocal(inside) {
			return nil, fmt.Errorf("output %q escapes its preopen", rel)
		}
		data, err := os.ReadFile(resolved)
		if err != nil {
			return nil, fmt.Errorf("output %q: %w", rel, err)
		}
		files[rel] = data
	}
	return files, nil
}

func commandArgs(m corpusModule) []string {
	argv0 := m.ID
	if m.Command.Argv0 != "" {
		argv0 = m.Command.Argv0
	}
	return append([]string{argv0}, m.Command.Args...)
}

func commandExitOK(err error) bool {
	if err == nil {
		return true
	}
	var wagoExit *wago.ExitError
	if errors.As(err, &wagoExit) {
		return wagoExit.Code == 0
	}
	var wazeroExit *wazerosys.ExitError
	return errors.As(err, &wazeroExit) && wazeroExit.ExitCode() == 0
}

func validateCommandOutput(m corpusModule, got commandOutput) error {
	hasStreamOracle := m.Command.StdoutSHA256 != "" || m.Command.StderrSHA256 != "" || len(m.Command.Outputs) != 0
	if m.Command.Oracle == "" && m.Command.Want == nil && !hasStreamOracle {
		return fmt.Errorf("command has no correctness oracle")
	}
	if m.Command.Want != nil && !slices.Equal(got.results, m.Command.Want) {
		return fmt.Errorf("results %v, want %v", got.results, m.Command.Want)
	}
	for _, stream := range []struct {
		name, want string
		got        []byte
	}{
		{name: "stdout", want: m.Command.StdoutSHA256, got: got.stdout},
		{name: "stderr", want: m.Command.StderrSHA256, got: got.stderr},
	} {
		if stream.want == "" {
			continue
		}
		if sum := fmt.Sprintf("%x", sha256.Sum256(stream.got)); sum != stream.want {
			preview := stream.got
			if len(preview) > 256 {
				preview = preview[:256]
			}
			return fmt.Errorf("%s sha256 %s, want %s (len=%d prefix=%q)", stream.name, sum, stream.want, len(stream.got), preview)
		}
	}
	for rel, want := range m.Command.Outputs {
		data, ok := got.files[rel]
		if !ok {
			return fmt.Errorf("missing output %q", rel)
		}
		if sum := fmt.Sprintf("%x", sha256.Sum256(data)); sum != want {
			return fmt.Errorf("output %q sha256 %s, want %s", rel, sum, want)
		}
	}
	return nil
}

func runWagoCommand(m corpusModule, compiled *wago.Compiled, stdin []byte, capture bool) (commandOutput, error) {
	preopenDir, cleanup, err := commandScratchPreopen(m)
	if err != nil {
		return commandOutput{}, err
	}
	defer cleanup()
	var stdout, stderr bytes.Buffer
	stdoutWriter, stderrWriter := io.Writer(io.Discard), io.Writer(io.Discard)
	if capture {
		stdoutWriter, stderrWriter = &stdout, &stderr
	}
	imports, err := commandRuntimeImports(m, preopenDir, stdin, stdoutWriter, stderrWriter)
	if err != nil {
		return commandOutput{}, err
	}
	in, err := wago.Instantiate(compiled, wago.InstantiateOptions{Imports: imports})
	if err != nil {
		return commandOutput{}, err
	}
	defer in.Close()
	results, err := in.Invoke(m.Command.Export)
	if !commandExitOK(err) {
		return commandOutput{results: results, stdout: stdout.Bytes(), stderr: stderr.Bytes()}, err
	}
	var files map[string][]byte
	if capture {
		files, err = commandOutputFiles(m, preopenDir)
		if err != nil {
			return commandOutput{results: results, stdout: stdout.Bytes(), stderr: stderr.Bytes()}, err
		}
	}
	return commandOutput{results: results, stdout: stdout.Bytes(), stderr: stderr.Bytes(), files: files}, nil
}

func runWazeroCommand(ctx context.Context, r wazero.Runtime, compiled wazero.CompiledModule, m corpusModule, stdin []byte, capture bool) (commandOutput, error) {
	preopenDir, cleanup, err := commandScratchPreopen(m)
	if err != nil {
		return commandOutput{}, err
	}
	defer cleanup()
	var stdout, stderr bytes.Buffer
	stdoutWriter, stderrWriter := io.Writer(io.Discard), io.Writer(io.Discard)
	if capture {
		stdoutWriter, stderrWriter = &stdout, &stderr
	}
	cfg := wazero.NewModuleConfig().WithName("").WithStartFunctions().WithArgs(commandArgs(m)...).
		WithStdin(bytes.NewReader(stdin)).WithStdout(stdoutWriter).WithStderr(stderrWriter).
		WithWalltime(func() (int64, int32) { return 0, 1 }, 1).
		WithNanotime(func() int64 { return 1 }, 1)
	fsCfg := wazero.NewFSConfig()
	if preopenDir != "" {
		fsCfg = fsCfg.WithDirMount(preopenDir, "/")
	}
	if m.Command.ReadOnlyPreopen != "" {
		fsCfg = fsCfg.WithReadOnlyDirMount(filepath.Join(corpusDir, m.Command.ReadOnlyPreopen), "/db")
	}
	if preopenDir != "" || m.Command.ReadOnlyPreopen != "" {
		cfg = cfg.WithFSConfig(fsCfg)
	}
	in, err := r.InstantiateModule(ctx, compiled, cfg)
	if err != nil {
		return commandOutput{}, err
	}
	defer in.Close(ctx)
	fn := in.ExportedFunction(m.Command.Export)
	if fn == nil {
		return commandOutput{}, fmt.Errorf("export %q not found", m.Command.Export)
	}
	results, err := fn.Call(ctx)
	if !commandExitOK(err) {
		return commandOutput{results: results, stdout: stdout.Bytes(), stderr: stderr.Bytes()}, err
	}
	var files map[string][]byte
	if capture {
		files, err = commandOutputFiles(m, preopenDir)
		if err != nil {
			return commandOutput{results: results, stdout: stdout.Bytes(), stderr: stderr.Bytes()}, err
		}
	}
	return commandOutput{results: results, stdout: stdout.Bytes(), stderr: stderr.Bytes(), files: files}, nil
}

func instantiateWazeroCommandHost(ctx context.Context, r wazero.Runtime, runtimeName string) error {
	switch runtimeName {
	case "core":
		return nil
	case "wasi":
		_, err := wazerowasi.Instantiate(ctx, r)
		return err
	case "ashell":
		builder := r.NewHostModuleBuilder(wazerowasi.ModuleName)
		wazerowasi.NewFunctionExporter().ExportFunctions(builder)
		builder.NewFunctionBuilder().WithFunc(func(_ context.Context, module api.Module, buf, bufLen, used uint32) uint32 {
			bytes, ok := ashellWazeroMemory(module)
			if !ok {
				return ashellErrnoFault
			}
			return ashellGetcwd(bytes, buf, bufLen, used)
		}).Export("ashell_getcwd")
		builder.NewFunctionBuilder().WithFunc(func(_ context.Context, module api.Module, name, nameLen, buf, bufLen, used uint32) uint32 {
			bytes, ok := ashellWazeroMemory(module)
			if !ok {
				return ashellErrnoFault
			}
			return ashellGetenv(bytes, name, nameLen, buf, bufLen, used)
		}).Export("ashell_getenv")
		builder.NewFunctionBuilder().WithFunc(func(uint32, uint32) uint32 { return ashellErrnoNosys }).Export("ashell_chdir")
		builder.NewFunctionBuilder().WithFunc(func(uint32, uint32) uint32 { return ashellErrnoNosys }).Export("ashell_system")
		_, err := builder.Instantiate(ctx)
		return err
	default:
		return fmt.Errorf("unsupported command runtime %q", runtimeName)
	}
}

func ashellWazeroMemory(module api.Module) ([]byte, bool) {
	memory := module.Memory()
	if memory == nil {
		return nil, false
	}
	return memory.Read(0, memory.Size())
}

func TestApplicationCorpusRuns(t *testing.T) {
	for _, m := range loadCorpus(t) {
		if m.Command == nil || !m.supports("CommandExec") {
			continue
		}
		m := m
		t.Run(m.name(), func(t *testing.T) {
			if !commandSupportsPlatform(m, runtime.GOOS, runtime.GOARCH) {
				t.Skipf("command adapter is not admitted on %s/%s; supported platforms: %v", runtime.GOOS, runtime.GOARCH, m.Command.Platforms)
			}
			validateCommandInputs(t, m)
			stdin := commandInput(t, m)
			t.Run("wago", func(t *testing.T) {
				compiled, err := wago.Compile(nil, m.bytes)
				if err != nil {
					t.Fatalf("compile: %v", err)
				}
				got, err := runWagoCommand(m, compiled, stdin, true)
				if err != nil {
					t.Fatalf("run: %v (stdout=%q stderr=%q)", err, got.stdout, got.stderr)
				}
				if err := validateCommandOutput(m, got); err != nil {
					t.Fatal(err)
				}
			})
			t.Run("wazero", func(t *testing.T) {
				ctx := context.Background()
				r := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfigCompiler())
				defer r.Close(ctx)
				if err := instantiateWazeroCommandHost(ctx, r, m.Command.Runtime); err != nil {
					t.Fatal(err)
				}
				compiled, err := r.CompileModule(ctx, m.bytes)
				if err != nil {
					t.Fatalf("compile: %v", err)
				}
				got, err := runWazeroCommand(ctx, r, compiled, m, stdin, true)
				if err != nil {
					t.Fatalf("run: %v (stdout=%q stderr=%q)", err, got.stdout, got.stderr)
				}
				if err := validateCommandOutput(m, got); err != nil {
					t.Fatal(err)
				}
			})
		})
	}
}

func BenchmarkCommandExec(b *testing.B) {
	for _, m := range commandCorpus(b) {
		compiled, err := wago.Compile(nil, m.bytes)
		if err != nil {
			b.Fatalf("%s compile: %v", m.name(), err)
		}
		stdin := commandInput(b, m)
		b.Run(m.name(), func(b *testing.B) {
			got, err := runWagoCommand(m, compiled, stdin, true)
			if err != nil {
				b.Fatal(err)
			}
			if err := validateCommandOutput(m, got); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := runWagoCommand(m, compiled, stdin, false); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkWazeroCommandExec(b *testing.B) {
	ctx := context.Background()
	for _, m := range commandCorpus(b) {
		m := m
		b.Run(m.name(), func(b *testing.B) {
			r := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfigCompiler())
			defer r.Close(ctx)
			if err := instantiateWazeroCommandHost(ctx, r, m.Command.Runtime); err != nil {
				b.Fatal(err)
			}
			compiled, err := r.CompileModule(ctx, m.bytes)
			if err != nil {
				b.Fatal(err)
			}
			defer compiled.Close(ctx)
			stdin := commandInput(b, m)
			got, err := runWazeroCommand(ctx, r, compiled, m, stdin, true)
			if err != nil {
				b.Fatal(err)
			}
			if err := validateCommandOutput(m, got); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := runWazeroCommand(ctx, r, compiled, m, stdin, false); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
