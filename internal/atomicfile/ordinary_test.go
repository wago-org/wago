package atomicfile

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestReplaceFileWithModeRejectsStagingPoliciesBeforeWriting(t *testing.T) {
	for name, options := range map[string]Options{
		"umask":    {ApplyUmask: true, Mode: 0o644},
		"retained": {RetainReplaceHandle: true},
		"both":     {ApplyUmask: true, RetainReplaceHandle: true, Mode: 0o644},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "value")
			if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
				t.Fatal(err)
			}
			err := ReplaceFileWithMode(path, options, func(io.Writer) error {
				t.Fatal("writer called for unsupported staging policy")
				return nil
			})
			if err == nil {
				t.Fatal("staging policy accepted")
			}
			if data, err := os.ReadFile(path); err != nil || string(data) != "old" {
				t.Fatalf("destination = %q, %v", data, err)
			}
			assertNoTemps(t, dir)
		})
	}
}

func TestReplaceFileWithModeFinalMode(t *testing.T) {
	for name, options := range map[string]Options{
		"default": {}, "explicit": {Mode: 0o640}, "zero": {ModeSet: true},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "value")
			if err := ReplaceFileWithMode(path, options, writeString("new")); err != nil {
				t.Fatal(err)
			}
			if runtime.GOOS != "windows" {
				want := options.Mode.Perm()
				if name == "default" {
					want = 0o600
				}
				info, err := os.Stat(path)
				if err != nil || info.Mode().Perm() != want {
					t.Fatalf("mode = %v, %v; want %o", info, err, want)
				}
			}
			assertNoTemps(t, dir)
		})
	}
}

func TestReplaceFileWithModeFailureBoundaries(t *testing.T) {
	injected := errors.New("injected failure")
	for name, options := range map[string]Options{
		"write":           {},
		"sync":            {Sync: true, Hooks: &Hooks{Sync: func(*os.File) error { return injected }}},
		"close":           {Hooks: &Hooks{Close: func(*os.File) error { return injected }}},
		"late-validation": {BeforeReplace: func(string) error { return injected }},
		"replace":         {Hooks: &Hooks{Replace: func(string, string) error { return injected }}},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "value")
			if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
				t.Fatal(err)
			}
			var staged *os.File
			err := ReplaceFileWithMode(path, options, func(writer io.Writer) error {
				staged = writer.(*os.File)
				if _, err := io.WriteString(writer, "partial replacement"); err != nil {
					return err
				}
				if name == "write" {
					return injected
				}
				return nil
			})
			if !errors.Is(err, injected) {
				t.Fatalf("failure = %v", err)
			}
			if staged == nil {
				t.Fatal("writer did not receive staged file")
			}
			if _, err := staged.Stat(); err == nil {
				t.Fatal("staged descriptor remains open")
			}
			if data, err := os.ReadFile(path); err != nil || string(data) != "old" {
				t.Fatalf("destination = %q, %v", data, err)
			}
			assertNoTemps(t, dir)
		})
	}
}

func BenchmarkOrdinaryPublication(b *testing.B) {
	for name, publish := range map[string]func(string, Options, func(io.Writer) error) error{
		"general": ReplaceFile, "final-mode": ReplaceFileWithMode,
	} {
		b.Run(name, func(b *testing.B) {
			path := filepath.Join(b.TempDir(), "value")
			data := make([]byte, 1024)
			write := func(writer io.Writer) error { _, err := writer.Write(data); return err }
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := publish(path, Options{Mode: 0o640}, write); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
