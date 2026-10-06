//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"errors"
	"runtime"
	"slices"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func TestRV64PublicSWARImportedMemoryQuotaContextAndRetiredOwner(t *testing.T) {
	producer, memory := importedMemoryProvider(t)
	source := rv64PublicSWARMemoryModule(true)
	if missing, err := Compile(nil, source); err != nil {
		t.Fatal(err)
	} else {
		defer missing.Close()
		if in, err := Instantiate(missing); in != nil || err == nil {
			t.Fatal("SIMD missing memory binding accepted")
		}
	}
	consumers := make([]*Instance, 2)
	for i := range consumers {
		config := NewRuntimeConfig().WithMemoryLimitPages(0)
		if i == 0 {
			config = config.WithMemoryLimitPages(2)
		}
		c, err := Compile(config, source)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		if i == 1 {
			blob, err := c.MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}
			loaded, err := LoadTrustedArtifact(blob)
			if loaded != nil {
				defer loaded.Close()
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := c.Close(); err != nil {
				t.Fatal(err)
			}
			c = loaded
		}
		consumers[i], err = Instantiate(c, NewImports().Memory("env", "memory", memory))
		if err != nil {
			t.Fatal(err)
		}
		defer consumers[i].Close()
		forwarded, err := consumers[i].ExportedMemory("memory")
		if err != nil || forwarded != memory {
			t.Fatal("SIMD importer changed original owner identity", err)
		}
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
	}
	runtime.GC()
	payload := []uint64{0x8899aabbccddeeff, 0x7ff0000000000001}
	a, b := consumers[0], consumers[1]
	if _, err := a.Invoke("store", 0xdeadbeef00000003, payload[0], payload[1]); err != nil {
		t.Fatal(err)
	}
	got, err := b.Invoke("load", 0xaabbccdd00000003)
	if err != nil || !slices.Equal(got, payload) {
		t.Fatal("raw vector memory import aliases", got, err)
	}
	tail := slices.Clone(memory.UnsafeBytes()[65520:65536])
	got, err = a.Invoke("store", 0xdeadbeef0000fff1, payload[0], payload[1])
	var trap *coreruntime.TrapError
	if !errors.As(err, &trap) || trap.Code != coreruntime.TrapLinMemOutOfBounds || got != nil || !slices.Equal(tail, memory.UnsafeBytes()[65520:65536]) {
		t.Fatal("imported full-width trap changed storage", got, err)
	}
	for _, test := range []struct {
		in          *Instance
		delta, want uint64
	}{{a, 1, 1}, {a, 1, 0xffffffff}, {b, 1, 2}, {a, 0, 3}, {a, 1, 0xffffffff}} {
		got, err = test.in.Invoke("grow", 0xdeadbeef00000000|test.delta, payload[0], payload[1])
		if err != nil || !slices.Equal(got, []uint64{payload[0], payload[1], test.want}) {
			t.Fatal("SIMD importer quota/current length/grow transport", test.delta, got, err)
		}
	}
	// The scalar owner and each vector importer have different private global
	// and data directories. Switching tenants must restore their own pointers.
	importedMemoryInvoke(t, producer, "bump", 11)
	state := []uint64{0x7ff0000000000001, 0x80000000deadbeef}
	got, err = a.Invoke("state")
	if err != nil || !slices.Equal(got, state) {
		t.Fatal("scalar provider displaced vector private globals", got, err)
	}
	got, err = a.Invoke("init", 0xdeadbeef00000065)
	if err != nil || !slices.Equal(got, state) {
		t.Fatal("importer passive segment/source indices", got, err)
	}
	importedMemoryInvoke(t, producer, "init", 0x5a, 101)
	got, err = b.Invoke("init", 0xdeadbeef00000065)
	if err != nil || !slices.Equal(got, state) {
		t.Fatal("private importer segment displaced by foreign initialization", got, err)
	}
	if err := producer.Close(); err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	if memory.jobMemory() == nil || memory.UnsafeBytes() != nil {
		t.Fatal("retired scalar memory owner did not retain vector borrowers")
	}
	// Use a fresh open code owner for the late-attachment rejection, so the
	// failure must come from the closed memory producer rather than code.Close.
	lateCode, err := Compile(nil, source)
	if err != nil {
		t.Fatal(err)
	}
	defer lateCode.Close()
	if late, err := Instantiate(lateCode, NewImports().Memory("env", "memory", memory)); late != nil || err == nil {
		t.Fatal("retired memory accepted a new vector importer")
	}
	got, err = b.Invoke("load", 0xdeadbeef00000065)
	if err != nil || !slices.Equal(got, state) {
		t.Fatal("vector borrower lost retired owner storage", got, err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	got, err = b.Invoke("grow", 0xdeadbeef00000001, payload[0], payload[1])
	if err != nil || !slices.Equal(got, []uint64{payload[0], payload[1], 3}) {
		t.Fatal("remaining vector borrower grow", got, err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	if memory.jobMemory() != nil {
		t.Fatal("final vector importer retained retired scalar mapping")
	}
}
func TestRV64PublicSWARImportedMemoryArtifactUnsupportedMetadata(t *testing.T) {
	c, err := Compile(nil, rv64PublicSWARMemoryModule(true))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	base := c.executionView()
	for _, test := range []struct {
		name   string
		mutate func(*Compiled)
	}{
		{"table-export", func(v *Compiled) { v.HasTable = true; v.TableSize = 1; v.tableExports = map[string]int{"private": 0} }},
		{"global-type-mismatch", func(v *Compiled) { v.GlobalImports = []GlobalImportDef{{Type: ValI64, Mutable: true}} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			view := *base
			test.mutate(&view)
			if err := railshotValidateCompiled(&view); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("unsupported imported SIMD state combination", err)
			}
		})
	}
	if c.codeCache.base != 0 {
		t.Fatal("rejected imported profile changed deferred ownership")
	}
}
