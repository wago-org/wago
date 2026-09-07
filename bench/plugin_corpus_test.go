package wagobench

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	emscripten "github.com/JairusSW/wago-emscripten"
	"github.com/wago-org/wago"
	"github.com/wago-org/wasi/p1"
	"github.com/wago-org/wasi/unstable"
)

// The JavaScript-ABI corpus needs the Emscripten compatibility plugin to reach
// instantiate and execution. Keep these rows in the same benchmark stream as
// the core corpus so benchpub and the website cannot silently omit them.
func BenchmarkPluginInstantiate(b *testing.B) {
	for _, m := range pluginCorpus(b) {
		b.Run(m.name(), func(b *testing.B) {
			rt, compiled, cleanup := compilePluginCorpus(b, m, nil)
			defer cleanup()
			defer rt.Close()
			defer compiled.Close()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				instance, err := rt.Instantiate(context.Background(), compiled)
				if err != nil {
					b.Fatal(err)
				}
				if err := instance.Close(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkPluginExec(b *testing.B) {
	for _, m := range pluginCorpus(b) {
		b.Run(m.name(), func(b *testing.B) {
			b.StopTimer()
			input := pluginInput(m.name())
			rt, compiled, cleanup := compilePluginCorpus(b, m, strings.NewReader(input))
			instance, err := rt.Instantiate(context.Background(), compiled)
			if err != nil {
				b.Fatal(err)
			}
			if m.name() == "lua" || m.name() == "sqlite3" || m.name() == "ruby" {
				if _, err := instance.InvokeContext(context.Background(), "_start"); err != nil {
					var exit *wago.ExitError
					if !errors.As(err, &exit) || exit.Code != 0 {
						b.Fatal(err)
					}
				}
			}
			started := time.Now()
			runPluginWorkload(b, m.name(), instance)
			b.ReportMetric(float64(time.Since(started).Nanoseconds()), "ns/op")
			if err := instance.Close(); err != nil {
				b.Fatal(err)
			}
			if err := compiled.Close(); err != nil {
				b.Fatal(err)
			}
			if err := rt.Close(); err != nil {
				b.Fatal(err)
			}
			cleanup()
		})
	}
}

func pluginCorpus(tb testing.TB) []corpusModule {
	wanted := map[string]bool{"regexmatch": true, "wasm3": true, "lua": true, "sqlite3": true, "ruby": true, "esbuild": true}
	var out []corpusModule
	for _, m := range loadCorpus(tb) {
		if wanted[m.name()] {
			out = append(out, m)
		}
	}
	if len(out) != len(wanted) {
		tb.Fatalf("plugin corpus has %d modules, want %d", len(out), len(wanted))
	}
	return out
}

func compilePluginCorpus(tb testing.TB, m corpusModule, stdin io.Reader) (*wago.Runtime, *wago.Module, func()) {
	tb.Helper()
	// Provider internals deliberately aren't exported. Redirect process streams
	// only while the provider is constructed, then restore them immediately.
	oldStdin, oldStdout, oldStderr := os.Stdin, os.Stdout, os.Stderr
	inputFile, err := os.CreateTemp("", "wago-plugin-bench-stdin-")
	if err != nil {
		tb.Fatal(err)
	}
	if stdin != nil {
		if _, err := io.Copy(inputFile, stdin); err != nil {
			tb.Fatal(err)
		}
	}
	if _, err := inputFile.Seek(0, 0); err != nil {
		tb.Fatal(err)
	}
	devnull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		tb.Fatal(err)
	}
	os.Stdin, os.Stdout, os.Stderr = inputFile, devnull, devnull
	restored := false
	restoreStreams := func() {
		if !restored {
			os.Stdin, os.Stdout, os.Stderr = oldStdin, oldStdout, oldStderr
			restored = true
		}
	}
	defer restoreStreams()
	provider := emscripten.Provider()
	rt := wago.NewRuntime(wago.WithGuestArguments(pluginArgs(m.name())))
	if err := rt.LoadPlugins(context.Background(), pluginSet(tb, provider)); err != nil {
		tb.Fatal(err)
	}
	restoreStreams()
	_ = os.Remove(inputFile.Name())
	compiled, err := rt.Compile(m.bytes)
	if err != nil {
		tb.Fatal(err)
	}
	cleanup := func() { _ = inputFile.Close(); _ = devnull.Close() }
	return rt, compiled, cleanup
}

func pluginSet(tb testing.TB, direct wago.PluginProvider) wago.PluginSet {
	tb.Helper()
	providers := []wago.PluginProvider{direct, p1.Provider(), unstable.Provider()}
	selections := make([]wago.PluginSelection, 0, len(providers))
	for i, provider := range providers {
		digest, err := wago.DefinitionDigest(provider.Definition)
		if err != nil {
			tb.Fatal(err)
		}
		grants := make([]wago.AuthorityGrant, 0, len(provider.Definition.Authorities))
		for _, request := range provider.Definition.Authorities {
			grants = append(grants, wago.AuthorityGrant{Name: request.Name, Scope: request.Scope})
		}
		dependencies := map[string]string{}
		for _, requirement := range provider.Definition.Requires {
			dependencies[requirement.ID] = requirement.Version
		}
		selections = append(selections, wago.PluginSelection{ID: provider.Definition.ID, DefinitionDigest: digest, Direct: i == 0, Grants: grants, Dependencies: dependencies})
	}
	return wago.PluginSet{Providers: providers, Selections: selections}
}

func pluginArgs(name string) []string {
	if name == "wasm3" {
		return []string{"wasm3.wasm", "--repl"}
	}
	if name == "esbuild" {
		return []string{"esbuild.wasm", "--loader=js", "--minify"}
	}
	return []string{name + ".wasm"}
}

func pluginInput(name string) string {
	if name == "wasm3" {
		return fmt.Sprintf(":load-hex %d\n%x\n:invoke fib 25\n:exit\n", len(pluginWasm3Workload), pluginWasm3Workload)
	}
	if name == "esbuild" {
		var source strings.Builder
		for i := 0; i < 1000; i++ {
			fmt.Fprintf(&source, "export function f%d(value) { const offset = %d; return value * %d + offset; }\n", i, i, i+1)
		}
		return source.String()
	}
	return ""
}

func runPluginWorkload(tb testing.TB, name string, instance *wago.Instance) {
	tb.Helper()
	switch name {
	case "lua":
		pluginLua(tb, instance)
	case "sqlite3":
		pluginSQLite(tb, instance)
	case "ruby":
		pluginRuby(tb, instance)
	default:
		_, err := instance.InvokeContext(context.Background(), "_start")
		var exit *wago.ExitError
		if err != nil && (!errors.As(err, &exit) || exit.Code != 0) {
			tb.Fatal(err)
		}
	}
}

func pluginLua(tb testing.TB, instance *wago.Instance) {
	state := pluginCallOne(tb, instance, "luaL_newstate")
	if state == 0 {
		tb.Fatal("luaL_newstate returned null")
	}
	defer pluginCall(tb, instance, "lua_close", state)
	pluginCall(tb, instance, "luaL_openlibs", state)
	source := append([]byte(`local s=0 for i=1,5000 do s=(s+i*i)%1000000007 end return s`), 0)
	p := pluginCallOne(tb, instance, "malloc", uint64(len(source)))
	defer pluginCall(tb, instance, "free", p)
	copy(instance.Memory().UnsafeBytes()[p:p+uint64(len(source))], source)
	if pluginCallOne(tb, instance, "luaL_loadstring", state, p) != 0 || pluginCallOne(tb, instance, "lua_pcallk", state, 0, 1, 0, 0, 0) != 0 {
		tb.Fatal("lua workload failed")
	}
}

func pluginSQLite(tb testing.TB, instance *wago.Instance) {
	if pluginCallOne(tb, instance, "sqlite3_initialize") != 0 {
		tb.Fatal("sqlite initialize failed")
	}
	memory := instance.Memory().UnsafeBytes()
	filename := pluginPutString(tb, instance, ":memory:")
	dbOut := pluginCallOne(tb, instance, "malloc", 4)
	if pluginCallOne(tb, instance, "sqlite3_open", filename, dbOut) != 0 {
		tb.Fatal("sqlite open failed")
	}
	db := uint64(binary.LittleEndian.Uint32(memory[dbOut : dbOut+4]))
	defer pluginCall(tb, instance, "sqlite3_close_v2", db)
	sql := pluginPutString(tb, instance, "CREATE TABLE t(x); WITH RECURSIVE s(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM s WHERE x<5000) INSERT INTO t SELECT x FROM s;")
	defer pluginCall(tb, instance, "free", sql)
	if pluginCallOne(tb, instance, "sqlite3_exec", db, sql, 0, 0, 0) != 0 {
		tb.Fatal("sqlite workload failed")
	}
}

func pluginRuby(tb testing.TB, instance *wago.Instance) {
	pluginCall(tb, instance, "ruby-init: func(args: list<string>) -> ()", 0, 0)
	source := []byte(`(1..2000).map { |n| n * n }.select(&:odd?).sum.to_s`)
	p := pluginCallOne(tb, instance, "cabi_realloc", 0, 0, 1, uint64(len(source)))
	copy(instance.Memory().UnsafeBytes()[p:p+uint64(len(source))], source)
	result := pluginCallOne(tb, instance, "rb-eval-string-protect: func(str: string) -> tuple<handle<rb-abi-value>, s32>", p, uint64(len(source)))
	memory := instance.Memory().UnsafeBytes()
	if binary.LittleEndian.Uint32(memory[result+4:result+8]) != 0 {
		tb.Fatal("ruby workload failed")
	}
	handle := uint64(binary.LittleEndian.Uint32(memory[result : result+4]))
	pluginCall(tb, instance, "canonical_abi_drop_rb-abi-value", handle)
}

func pluginPutString(tb testing.TB, instance *wago.Instance, value string) uint64 {
	data := append([]byte(value), 0)
	p := pluginCallOne(tb, instance, "malloc", uint64(len(data)))
	copy(instance.Memory().UnsafeBytes()[p:p+uint64(len(data))], data)
	return p
}

func pluginCallOne(tb testing.TB, instance *wago.Instance, export string, args ...uint64) uint64 {
	results := pluginCall(tb, instance, export, args...)
	if len(results) != 1 {
		tb.Fatalf("%s returned %d results", export, len(results))
	}
	return results[0]
}

func pluginCall(tb testing.TB, instance *wago.Instance, export string, args ...uint64) []uint64 {
	results, err := instance.InvokeContext(context.Background(), export, args...)
	if err != nil {
		tb.Fatalf("%s: %v", export, err)
	}
	return results
}

var pluginWasm3Workload = []byte{
	0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00, 0x01, 0x09, 0x02, 0x60,
	0x01, 0x7f, 0x01, 0x7f, 0x60, 0x00, 0x00, 0x03, 0x03, 0x02, 0x00, 0x01,
	0x07, 0x10, 0x02, 0x03, 0x66, 0x69, 0x62, 0x00, 0x00, 0x06, 0x5f, 0x73,
	0x74, 0x61, 0x72, 0x74, 0x00, 0x01, 0x0a, 0x2e, 0x02, 0x1c, 0x00, 0x20,
	0x00, 0x41, 0x02, 0x48, 0x04, 0x7f, 0x20, 0x00, 0x05, 0x20, 0x00, 0x41,
	0x01, 0x6b, 0x10, 0x00, 0x20, 0x00, 0x41, 0x02, 0x6b, 0x10, 0x00, 0x6a,
	0x0b, 0x0b, 0x0f, 0x00, 0x41, 0x19, 0x10, 0x00, 0x41, 0x91, 0xca, 0x04,
	0x47, 0x04, 0x40, 0x00, 0x0b, 0x0b,
}
