package wago

import "testing"

var reservedModuleBenchSink bool

func TestReservedModulesStayExact(t *testing.T) {
	reserved := []string{
		"wago_process", "wago_mailbox", "wago_timer", "wago_metrics",
		"wago_log", "wago_fs", "wago_net", "wago_http", "wago_kv",
		"wago_crypto", "wago_debug", "wago_runtime",
	}
	for _, module := range reserved {
		if !isReserved(module) {
			t.Errorf("reserved module %q was not recognized", module)
		}
	}
	for _, module := range []string{"", "wago", "wago_process_extra", "env", "wasi_snapshot_preview1"} {
		if isReserved(module) {
			t.Errorf("ordinary module %q was marked reserved", module)
		}
	}
}

func BenchmarkReservedModuleLookup(b *testing.B) {
	modules := [...]string{
		"wago_process", "env", "wago_runtime", "wasi_snapshot_preview1",
		"wago_http", "wago_process_extra", "wago_crypto", "",
	}
	var reserved bool
	b.ReportMetric(256, "lookups/op")
	for i := 0; i < b.N; i++ {
		for j := 0; j < 256; j++ {
			reserved = isReserved(modules[j%len(modules)])
		}
	}
	reservedModuleBenchSink = reserved
}
