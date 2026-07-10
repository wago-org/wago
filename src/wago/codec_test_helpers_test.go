package wago

import "testing"

func writeCompiledCodecPrefixAfterFuncs(t testing.TB, w *compiledWriter) {
	t.Helper()
	w.bytes(nil)
	w.intSlice(nil) // Entry.
	w.intSlice(nil) // InternalEntry.
	w.uvar(0)       // NumImports.
	w.stringSlice(nil)
	if err := w.funcSigs(nil); err != nil {
		t.Fatalf("write import funcs: %v", err)
	}
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
	w.uvar(0) // tables.
	w.stringIntMap(nil)
	w.u32Slice(nil)
	w.bool(false) // NeedsFuncRefDescs.
}

func writeCompiledCodecElementPrefix(w *compiledWriter) {
	w.uvar(1)
	w.u32(0)
	w.u8(0x70) // funcref.
	w.u8(byte(ElemModeActive))
	w.offset(OffsetInit{})
}

func writeCompiledCodecPrefixAfterMemoryImport(t testing.TB, w *compiledWriter) {
	t.Helper()
	writeCompiledCodecPrefixAfterFuncTypeIDs(t, w)
	w.elems(nil) // active element segments.
	w.elems(nil) // passive element segments.
	w.data(nil)
	w.passiveData(nil)
	w.str("") // memoryImport.
	w.u8(0)   // requiredFeatures.
}
