//go:build (darwin && arm64) || (linux && amd64)

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
	"slices"
	"strings"
	"testing"

	"github.com/tetratelabs/wazero"
	wazerowasi "github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	wazerosys "github.com/tetratelabs/wazero/sys"
	"github.com/wago-org/wago"
	"github.com/wago-org/wasi/p1"
)

type commandOutput struct {
	results        []uint64
	stdout, stderr []byte
}

func commandCorpus(tb testing.TB) []corpusModule {
	tb.Helper()
	var out []corpusModule
	for _, m := range loadCorpus(tb) {
		if m.Command != nil && m.supports("CommandExec") {
			out = append(out, m)
		}
	}
	return out
}

func commandInput(tb testing.TB, path string) []byte {
	tb.Helper()
	if path == "" {
		return nil
	}
	b, err := os.ReadFile(filepath.Join(corpusDir, path))
	if err != nil {
		tb.Fatalf("read command input %s: %v", path, err)
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
	return append([]string{m.File}, m.Command.Args...)
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
	var imports wago.Imports
	if m.Command.Runtime == "wasi" {
		cfg := p1.Config{
			Args: commandArgs(m), Stdin: bytes.NewReader(stdin),
			Stdout: stdoutWriter, Stderr: stderrWriter,
			Now: func() int64 { return 0 },
		}
		if dir := commandPreopen(m); dir != "" {
			cfg.Preopens = map[string]string{"/": dir}
		}
		imports = p1.Imports(cfg)
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
			stdin := commandInput(t, m.Command.Stdin)
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

func TestApplicationCorpusChecksums(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(corpusDir, "APPLICATION_SHA256SUMS"))
	if err != nil {
		t.Fatal(err)
	}
	checked := make(map[string]bool)
	for lineNum, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			t.Fatalf("checksum line %d: got %q", lineNum+1, line)
		}
		data, err := os.ReadFile(filepath.Join(corpusDir, fields[1]))
		if err != nil {
			t.Fatalf("read %s: %v", fields[1], err)
		}
		if got := fmt.Sprintf("%x", sha256.Sum256(data)); got != fields[0] {
			t.Fatalf("%s sha256 %s, want %s", fields[1], got, fields[0])
		}
		if checked[fields[1]] {
			t.Fatalf("duplicate checksum for %s", fields[1])
		}
		checked[fields[1]] = true
	}
	for _, m := range commandCorpus(t) {
		if !checked[m.File] {
			t.Errorf("%s has no checksum", m.File)
		}
	}
	err = filepath.WalkDir(filepath.Join(corpusDir, "inputs"), func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		rel, err := filepath.Rel(corpusDir, path)
		if err != nil {
			return err
		}
		if !checked[rel] {
			t.Errorf("%s has no checksum", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func BenchmarkCommandExec(b *testing.B) {
	for _, m := range commandCorpus(b) {
		compiled, err := wago.Compile(nil, m.bytes)
		if err != nil {
			b.Fatalf("%s compile: %v", m.name(), err)
		}
		stdin := commandInput(b, m.Command.Stdin)
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
			stdin := commandInput(b, m.Command.Stdin)
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
