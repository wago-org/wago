package wago

import "testing"

func writeCompiledCodecPrefixAfterFuncs(t testing.TB, w *compiledWriter) {
	t.Helper()
	w.bytes(nil)
	w.intSlice(nil)
	w.uvar(0) // NumImports.
	w.stringSlice(nil)
	if err := w.funcSigs(nil); err != nil {
		t.Fatalf("write funcs: %v", err)
	}
}

func writeCompiledCodecPrefixAfterExports(t testing.TB, w *compiledWriter) {
	t.Helper()
	writeCompiledCodecPrefixAfterFuncs(t, w)
	w.stringIntMap(nil)
}

func writeCompiledCodecPrefixAfterGlobalExports(t testing.TB, w *compiledWriter) {
	t.Helper()
	writeCompiledCodecPrefixAfterExports(t, w)
	w.nameSec(nil)
	if err := w.globalImports(nil); err != nil {
		t.Fatalf("write global imports: %v", err)
	}
	if err := w.globals(nil); err != nil {
		t.Fatalf("write globals: %v", err)
	}
	w.stringIntMap(nil)
}

func writeCompiledCodecPrefixAfterFuncTypeIDs(t testing.TB, w *compiledWriter) {
	t.Helper()
	writeCompiledCodecPrefixAfterGlobalExports(t, w)
	w.bool(false)
	w.uvar(0) // TableSize.
	w.u32Slice(nil)
}

func writeCompiledCodecPrefixAfterMemoryImport(t testing.TB, w *compiledWriter) {
	t.Helper()
	writeCompiledCodecPrefixAfterFuncTypeIDs(t, w)
	w.elems(nil)
	w.data(nil)
	w.str("")
}
