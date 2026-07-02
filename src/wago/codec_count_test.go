package wago

import (
	"strings"
	"testing"
)

func TestCompiledReaderRejectsMaliciousNameSectionCountsBeforeAllocation(t *testing.T) {
	huge := uint64(maxInt())

	writeNameSecPrefix := func(w *compiledWriter) {
		writeCompiledCodecPrefixAfterExports(t, w)
		w.bool(true)  // Name section present.
		w.bool(false) // No module name.
	}
	writeEmptyNameMap := func(w *compiledWriter) { w.uvar(0) }
	writeEmptyIndirectNameMap := func(w *compiledWriter) { w.uvar(0) }
	writeNameMapEntryPrefix := func(w *compiledWriter) {
		w.uvar(1)
		w.u32(0)
	}
	writeIndirectNameMapEntryPrefix := func(w *compiledWriter) {
		w.uvar(1)
		w.u32(0)
	}
	writeThroughLocalNames := func(w *compiledWriter) {
		writeEmptyNameMap(w)         // FunctionNames.
		writeEmptyIndirectNameMap(w) // LocalNames.
	}
	writeThroughLabelNames := func(w *compiledWriter) {
		writeThroughLocalNames(w)
		writeEmptyIndirectNameMap(w) // LabelNames.
	}
	writeThroughDataNames := func(w *compiledWriter) {
		writeThroughLabelNames(w)
		writeEmptyNameMap(w) // TypeNames.
		writeEmptyNameMap(w) // TableNames.
		writeEmptyNameMap(w) // MemoryNames.
		writeEmptyNameMap(w) // GlobalNames.
		writeEmptyNameMap(w) // ElementNames.
		writeEmptyNameMap(w) // DataNames.
	}

	tests := []struct {
		name  string
		write func(*compiledWriter)
	}{
		{
			name: "module name length",
			write: func(w *compiledWriter) {
				writeCompiledCodecPrefixAfterExports(t, w)
				w.bool(true) // Name section present.
				w.bool(true) // Module name present.
				w.uvar(huge)
			},
		},
		{
			name: "function name count",
			write: func(w *compiledWriter) {
				writeNameSecPrefix(w)
				w.uvar(huge)
			},
		},
		{
			name: "function name length",
			write: func(w *compiledWriter) {
				writeNameSecPrefix(w)
				writeNameMapEntryPrefix(w)
				w.uvar(huge)
			},
		},
		{
			name: "local indirect count",
			write: func(w *compiledWriter) {
				writeNameSecPrefix(w)
				writeEmptyNameMap(w) // FunctionNames.
				w.uvar(huge)
			},
		},
		{
			name: "local nested name count",
			write: func(w *compiledWriter) {
				writeNameSecPrefix(w)
				writeEmptyNameMap(w) // FunctionNames.
				writeIndirectNameMapEntryPrefix(w)
				w.uvar(huge)
			},
		},
		{
			name: "local nested name length",
			write: func(w *compiledWriter) {
				writeNameSecPrefix(w)
				writeEmptyNameMap(w) // FunctionNames.
				writeIndirectNameMapEntryPrefix(w)
				writeNameMapEntryPrefix(w)
				w.uvar(huge)
			},
		},
		{
			name: "label indirect count",
			write: func(w *compiledWriter) {
				writeNameSecPrefix(w)
				writeThroughLocalNames(w)
				w.uvar(huge)
			},
		},
		{
			name: "label nested name count",
			write: func(w *compiledWriter) {
				writeNameSecPrefix(w)
				writeThroughLocalNames(w)
				writeIndirectNameMapEntryPrefix(w)
				w.uvar(huge)
			},
		},
		{
			name: "type name count",
			write: func(w *compiledWriter) {
				writeNameSecPrefix(w)
				writeThroughLabelNames(w)
				w.uvar(huge)
			},
		},
		{
			name: "table name count",
			write: func(w *compiledWriter) {
				writeNameSecPrefix(w)
				writeThroughLabelNames(w)
				writeEmptyNameMap(w) // TypeNames.
				w.uvar(huge)
			},
		},
		{
			name: "memory name count",
			write: func(w *compiledWriter) {
				writeNameSecPrefix(w)
				writeThroughLabelNames(w)
				writeEmptyNameMap(w) // TypeNames.
				writeEmptyNameMap(w) // TableNames.
				w.uvar(huge)
			},
		},
		{
			name: "global name count",
			write: func(w *compiledWriter) {
				writeNameSecPrefix(w)
				writeThroughLabelNames(w)
				writeEmptyNameMap(w) // TypeNames.
				writeEmptyNameMap(w) // TableNames.
				writeEmptyNameMap(w) // MemoryNames.
				w.uvar(huge)
			},
		},
		{
			name: "element name count",
			write: func(w *compiledWriter) {
				writeNameSecPrefix(w)
				writeThroughLabelNames(w)
				writeEmptyNameMap(w) // TypeNames.
				writeEmptyNameMap(w) // TableNames.
				writeEmptyNameMap(w) // MemoryNames.
				writeEmptyNameMap(w) // GlobalNames.
				w.uvar(huge)
			},
		},
		{
			name: "data name count",
			write: func(w *compiledWriter) {
				writeNameSecPrefix(w)
				writeThroughLabelNames(w)
				writeEmptyNameMap(w) // TypeNames.
				writeEmptyNameMap(w) // TableNames.
				writeEmptyNameMap(w) // MemoryNames.
				writeEmptyNameMap(w) // GlobalNames.
				writeEmptyNameMap(w) // ElementNames.
				w.uvar(huge)
			},
		},
		{
			name: "field indirect count",
			write: func(w *compiledWriter) {
				writeNameSecPrefix(w)
				writeThroughDataNames(w)
				w.uvar(huge)
			},
		},
		{
			name: "field nested name count",
			write: func(w *compiledWriter) {
				writeNameSecPrefix(w)
				writeThroughDataNames(w)
				writeIndirectNameMapEntryPrefix(w)
				w.uvar(huge)
			},
		},
		{
			name: "field nested name length",
			write: func(w *compiledWriter) {
				writeNameSecPrefix(w)
				writeThroughDataNames(w)
				writeIndirectNameMapEntryPrefix(w)
				writeNameMapEntryPrefix(w)
				w.uvar(huge)
			},
		},
		{
			name: "tag name count",
			write: func(w *compiledWriter) {
				writeNameSecPrefix(w)
				writeThroughDataNames(w)
				writeEmptyIndirectNameMap(w) // FieldNames.
				w.uvar(huge)
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
				t.Fatal("unmarshalCompiled accepted malicious name section")
			}
			if got := err.Error(); !strings.Contains(got, "count") && !strings.Contains(got, "capacity") {
				t.Fatalf("unmarshalCompiled error = %v, want count/capacity rejection", err)
			}
		})
	}
}

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
