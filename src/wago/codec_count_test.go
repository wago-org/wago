package wago

import (
	"strings"
	"testing"
)

func TestCompiledReaderRejectsMaliciousCountsBeforeAllocation(t *testing.T) {
	huge := uint64(maxInt())

	tests := []struct {
		name  string
		write func(*compiledWriter)
	}{
		{
			name: "code bytes",
			write: func(w *compiledWriter) {
				w.uvar(huge)
			},
		},
		{
			name: "entry slice",
			write: func(w *compiledWriter) {
				w.bytes(nil)
				w.uvar(huge)
			},
		},
		{
			name: "imports slice",
			write: func(w *compiledWriter) {
				w.bytes(nil)
				w.intSlice(nil)
				w.uvar(0) // NumImports.
				w.uvar(huge)
			},
		},
		{
			name: "function signatures",
			write: func(w *compiledWriter) {
				w.bytes(nil)
				w.intSlice(nil)
				w.uvar(0) // NumImports.
				w.stringSlice(nil)
				w.uvar(huge)
			},
		},
		{
			name: "function parameters",
			write: func(w *compiledWriter) {
				w.bytes(nil)
				w.intSlice(nil)
				w.uvar(0) // NumImports.
				w.stringSlice(nil)
				w.uvar(1)
				w.uvar(huge)
			},
		},
		{
			name: "function results",
			write: func(w *compiledWriter) {
				w.bytes(nil)
				w.intSlice(nil)
				w.uvar(0) // NumImports.
				w.stringSlice(nil)
				w.uvar(1)
				w.uvar(0)
				w.uvar(huge)
			},
		},
		{
			name: "exports map",
			write: func(w *compiledWriter) {
				writeCompiledCodecPrefixAfterFuncs(t, w)
				w.uvar(huge)
			},
		},
		{
			name: "global imports",
			write: func(w *compiledWriter) {
				writeCompiledCodecPrefixAfterExports(t, w)
				w.nameSec(nil)
				w.uvar(huge)
			},
		},
		{
			name: "globals",
			write: func(w *compiledWriter) {
				writeCompiledCodecPrefixAfterExports(t, w)
				w.nameSec(nil)
				if err := w.globalImports(nil); err != nil {
					t.Fatalf("write global imports: %v", err)
				}
				w.uvar(huge)
			},
		},
		{
			name: "function type IDs",
			write: func(w *compiledWriter) {
				writeCompiledCodecPrefixAfterGlobalExports(t, w)
				w.bool(false)
				w.uvar(0) // TableSize.
				w.uvar(huge)
			},
		},
		{
			name: "element segments",
			write: func(w *compiledWriter) {
				writeCompiledCodecPrefixAfterFuncTypeIDs(t, w)
				w.uvar(huge)
			},
		},
		{
			name: "element functions",
			write: func(w *compiledWriter) {
				writeCompiledCodecPrefixAfterFuncTypeIDs(t, w)
				w.uvar(1)
				w.offset(OffsetInit{})
				w.uvar(huge)
			},
		},
		{
			name: "data segments",
			write: func(w *compiledWriter) {
				writeCompiledCodecPrefixAfterFuncTypeIDs(t, w)
				w.elems(nil)
				w.uvar(huge)
			},
		},
		{
			name: "data bytes",
			write: func(w *compiledWriter) {
				writeCompiledCodecPrefixAfterFuncTypeIDs(t, w)
				w.elems(nil)
				w.uvar(1)
				w.offset(OffsetInit{})
				w.uvar(huge)
			},
		},
		{
			name: "memory import string",
			write: func(w *compiledWriter) {
				writeCompiledCodecPrefixAfterFuncTypeIDs(t, w)
				w.elems(nil)
				w.data(nil)
				w.uvar(huge)
			},
		},
		{
			name: "GC type descriptors",
			write: func(w *compiledWriter) {
				writeCompiledCodecPrefixAfterMemoryImport(t, w)
				w.uvar(huge)
			},
		},
		{
			name: "GC type fields",
			write: func(w *compiledWriter) {
				writeCompiledCodecPrefixAfterMemoryImport(t, w)
				w.uvar(1)
				w.u32(0)     // ID.
				w.u8(0)      // Kind.
				w.bool(true) // Fields are present.
				w.uvar(huge)
				for i := 0; i < minGCDescTailBytes; i++ {
					w.u8(0)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var w compiledWriter
			tt.write(&w)
			var c Compiled
			err := unmarshalCompiled(&c, w.buf)
			if err == nil {
				t.Fatal("unmarshalCompiled accepted malicious count")
			}
			if !strings.Contains(err.Error(), "count") {
				t.Fatalf("unmarshalCompiled error = %v, want count rejection", err)
			}
		})
	}
}
