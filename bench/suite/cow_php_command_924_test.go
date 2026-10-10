//go:build linux && (amd64 || arm64)

package wagobench

import (
	"bytes"
	"reflect"
	"runtime"
	"slices"
	"testing"

	wago "github.com/wago-org/wago"
)

func cowPHPCommand(tb testing.TB) corpusModule {
	tb.Helper()
	for _, m := range readCatalogForSelector(tb, "php-buckets") {
		if m.ID == "php-buckets" && m.Command != nil && m.supports("CommandExec") &&
			commandSupportsPlatform(m, runtime.GOOS, runtime.GOARCH) {
			validateCommandInputs(tb, m)
			return m
		}
	}
	tb.Fatal("missing php-buckets corpus command")
	return corpusModule{}
}

// TestCowPHPRealCommand runs PHP's actual WASI command and output oracle with
// explicit bounds in both modes, rather than just its data initializer.
func TestCowPHPRealCommand(t *testing.T) {
	m := cowPHPCommand(t)
	stdin := commandInput(t, m)
	var baseline commandOutput
	for _, mode := range []struct{ name, flag string }{{"baseline", "0"}, {"cow", "1"}} {
		t.Run(mode.name, func(t *testing.T) {
			t.Setenv("WAGO_EXPERIMENT_COW_IMAGE", mode.flag)
			c, err := wago.Compile(wago.NewRuntimeConfig().WithBoundsChecks(wago.BoundsChecksExplicit), m.bytes)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			for repeat := 0; repeat < 2; repeat++ {
				got, err := runWagoCommand(m, c, stdin, true)
				if err != nil {
					t.Fatalf("real PHP run %d: %v", repeat, err)
				}
				if err := validateCommandOutput(m, got); err != nil {
					t.Fatalf("PHP oracle run %d: %v", repeat, err)
				}
				if mode.flag == "0" {
					baseline = got
				} else if !slices.Equal(baseline.results, got.results) || !bytes.Equal(baseline.stdout, got.stdout) || !bytes.Equal(baseline.stderr, got.stderr) || !reflect.DeepEqual(baseline.files, got.files) {
					t.Fatal("CoW PHP output differs from baseline")
				}
			}
		})
	}
}

// BenchmarkCowPHPRealCommand includes fresh instance creation and actual PHP
// command execution, but excludes compilation. Both modes use the same input.
func BenchmarkCowPHPRealCommand(b *testing.B) {
	m := cowPHPCommand(b)
	stdin := commandInput(b, m)
	for _, mode := range []struct{ name, flag string }{{"baseline", "0"}, {"cow", "1"}} {
		b.Run(mode.name, func(b *testing.B) {
			b.Setenv("WAGO_EXPERIMENT_COW_IMAGE", mode.flag)
			c, err := wago.Compile(wago.NewRuntimeConfig().WithBoundsChecks(wago.BoundsChecksExplicit), m.bytes)
			if err != nil {
				b.Fatal(err)
			}
			defer c.Close()
			for warmups := 0; warmups < 2; warmups++ {
				got, err := runWagoCommand(m, c, stdin, true)
				if err != nil {
					b.Fatal(err)
				}
				if err := validateCommandOutput(m, got); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := runWagoCommand(m, c, stdin, false); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
