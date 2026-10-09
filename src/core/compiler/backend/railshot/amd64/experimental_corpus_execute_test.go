//go:build linux && amd64

package amd64

import (
	"encoding/binary"
	"os"
	"testing"
)

// This corpus loop already has the native four-chain sum path. It is a
// negative control for replacing that path with general ordered replication.
func BenchmarkExperimentalCorpusExecute(b *testing.B) {
	m := experimentCorpusModule(b, "corpus/workloads/synthetic/memory.wasm")
	opts := CompileOptions{ExperimentalLoopMode: os.Getenv("WAGO_LOOP_REPLICATION"), Workers: 1}
	cm, err := CompileModuleWith(m, opts)
	if err != nil {
		b.Fatal(err)
	}
	defer cm.CodeImage.Close()
	r := newLoopExperimentRun(b, m, opts, 65536)
	// The harness starts at local function zero (fill). Use the sum export's
	// offset from a compilation with identical options and source.
	r.entry += uintptr(cm.Entry[1] - cm.Entry[0])
	for i := 0; i < 8192; i++ {
		binary.LittleEndian.PutUint64(r.jm.CurrentBytes()[i*8:], uint64(i+1))
	}
	if err := r.call(8192); err != nil {
		b.Fatal(err)
	}
	const want = 33558528
	if binary.LittleEndian.Uint64(r.out) != want {
		b.Fatal("incorrect corpus sum")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := r.eng.Call(r.entry, r.args, r.jm.LinearMemory(), r.trap, r.out); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	if binary.LittleEndian.Uint64(r.out) != want {
		b.Fatal("final corpus sum differs")
	}
}
