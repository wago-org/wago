package wagobench

import (
	"testing"

	wago "github.com/wago-org/wago"
)

// BenchmarkRepeatLoadPHPCommand runs the unchanged PHP corpus command. Select
// baseline or experimental code in a separate process with the environment
// flag, which the AMD64 compiler reads at initialization.
func BenchmarkRepeatLoadPHPCommand(b *testing.B) {
	var m corpusModule
	for _, candidate := range commandCorpus(b) {
		if candidate.ID == "php-buckets" {
			m = candidate
			break
		}
	}
	if m.ID == "" {
		b.Fatal("missing php-buckets command")
	}
	stdin := commandInput(b, m)
	c, err := wago.Compile(nil, m.bytes)
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	got, err := runWagoCommand(m, c, stdin, true)
	if err != nil {
		b.Fatal(err)
	}
	if err := validateCommandOutput(m, got); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := runWagoCommand(m, c, stdin, false); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRepeatLoadPHPCompile uses the identical real Wasm bytes in both
// separate-process modes, including Wago's normal validation and code mapping.
func BenchmarkRepeatLoadPHPCompile(b *testing.B) {
	var m corpusModule
	for _, candidate := range commandCorpus(b) {
		if candidate.ID == "php-buckets" {
			m = candidate
			break
		}
	}
	if m.ID == "" {
		b.Fatal("missing php-buckets command")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c, err := wago.Compile(nil, m.bytes)
		if err != nil {
			b.Fatal(err)
		}
		if err := c.Close(); err != nil {
			b.Fatal(err)
		}
	}
}
