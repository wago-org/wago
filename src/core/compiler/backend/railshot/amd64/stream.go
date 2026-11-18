//go:build amd64 && !tinygo && !wago_profile

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"os"
)

var streamingOperandsEnabled = os.Getenv("WAGO_EXPERIMENTAL_STREAM_OPERANDS") == "1"

func (f *fn) admitStreaming(c *wasm.Func) streamSummary {
	if !streamingOperandsEnabled || regallocCheckEnabled || f.nLocals > 256 || f.moduleEH || f.gcFrameRoots != nil || len(f.gcTypeLayouts) != 0 || len(f.customInstructions) != 0 {
		return streamSummary{}
	}
	summary := admitStreamBody(c, f.ft)
	if summary.Memory && (f.m.MemCount() != 1 || f.memoryAddr64(0) || f.threadedMemory0) {
		return streamSummary{}
	}
	return summary
}

func (f *fn) streamBody(c *wasm.Func) error {
	var widths [256]bool
	for i, t := range f.localType {
		widths[i] = t == mtI64
	}
	f.stats.peep("streaming-function")
	slots, err := f.sc.stream.CompileStream(c.BodyBytes, f.streamSummary, widths[:f.nLocals], f.nParams, f)
	if slots > f.maxSpill {
		f.maxSpill = slots
	}
	if err == nil && f.representationLimit != functionRepresentationOK {
		return f.representationError()
	}
	return err
}
