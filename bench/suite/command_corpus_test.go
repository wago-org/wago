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
	wazerowasi "github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	wazerosys "github.com/tetratelabs/wazero/sys"
	"github.com/wago-org/wago"
)

type commandOutput struct {
	results        []uint64
	stdout, stderr []byte
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
	if m.Command.Preopen == "" {
		if len(m.Command.Inputs) != 0 {
			tb.Fatalf("%s declares inputs without a preopen directory", m.ID)
		}
		return
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

func commandArgs(m corpusModule) []string {
	return append([]string{m.ID}, m.Command.Args...)
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
	hasStreamOracle := m.Command.StdoutSHA256 != "" || m.Command.StderrSHA256 != ""
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
	return nil
}

func runWagoCommand(m corpusModule, compiled *wago.Compiled, stdin []byte, capture bool) (commandOutput, error) {
	var stdout, stderr bytes.Buffer
	stdoutWriter, stderrWriter := io.Writer(io.Discard), io.Writer(io.Discard)
	if capture {
		stdoutWriter, stderrWriter = &stdout, &stderr
	}
	imports, err := commandRuntimeImports(m, stdin, stdoutWriter, stderrWriter)
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
	return commandOutput{results: results, stdout: stdout.Bytes(), stderr: stderr.Bytes()}, nil
}

func runWazeroCommand(ctx context.Context, r wazero.Runtime, compiled wazero.CompiledModule, m corpusModule, stdin []byte, capture bool) (commandOutput, error) {
	var stdout, stderr bytes.Buffer
	stdoutWriter, stderrWriter := io.Writer(io.Discard), io.Writer(io.Discard)
	if capture {
		stdoutWriter, stderrWriter = &stdout, &stderr
	}
	cfg := wazero.NewModuleConfig().WithName("").WithStartFunctions().WithArgs(commandArgs(m)...).
		WithStdin(bytes.NewReader(stdin)).WithStdout(stdoutWriter).WithStderr(stderrWriter).
		WithWalltime(func() (int64, int32) { return 0, 0 }, 1).
		WithNanotime(func() int64 { return 0 }, 1)
	if dir := commandPreopen(m); dir != "" {
		cfg = cfg.WithFSConfig(wazero.NewFSConfig().WithDirMount(dir, "/"))
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
	return commandOutput{results: results, stdout: stdout.Bytes(), stderr: stderr.Bytes()}, nil
}

func TestApplicationCorpusRuns(t *testing.T) {
	for _, m := range commandCorpus(t) {
		m := m
		t.Run(m.name(), func(t *testing.T) {
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
				if m.Command.Runtime == "wasi" {
					if _, err := wazerowasi.Instantiate(ctx, r); err != nil {
						t.Fatal(err)
					}
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
			if m.Command.Runtime == "wasi" {
				if _, err := wazerowasi.Instantiate(ctx, r); err != nil {
					b.Fatal(err)
				}
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
