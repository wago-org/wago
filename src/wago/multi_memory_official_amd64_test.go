//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago/internal/spectest"
)

type stagedOfficialSpecFile struct {
	Commands []struct {
		Type     string `json:"type"`
		Filename string `json:"filename"`
	} `json:"commands"`
}

func stagedOfficialMultiMemoryModules(t *testing.T, base string) [][]byte {
	t.Helper()
	checkout := filepath.Clean("../../tests/spec-v3")
	suite, err := spectest.DiscoverRelease3(checkout)
	if err != nil {
		t.Fatalf("discover pinned Release 3 suite: %v", err)
	}
	wast2json, err := exec.LookPath("wast2json")
	if err != nil {
		t.Skip("wast2json (WABT 1.0.41) not on PATH")
	}
	out, err := exec.Command(wast2json, "--version").CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "1.0.41" {
		t.Fatalf("wast2json version = %q, %v; want pinned 1.0.41", strings.TrimSpace(string(out)), err)
	}
	tmp := t.TempDir()
	jsonPath := filepath.Join(tmp, base+".json")
	wast := filepath.Join(suite.CoreDir, "multi-memory", base+".wast")
	if out, err := exec.Command(wast2json, "--enable-all", wast, "-o", jsonPath).CombinedOutput(); err != nil {
		t.Fatalf("convert pinned %s.wast: %v: %s", base, err, out)
	}
	raw, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	var sf stagedOfficialSpecFile
	if err := json.Unmarshal(raw, &sf); err != nil {
		t.Fatalf("decode %s JSON: %v", base, err)
	}
	var modules [][]byte
	for _, c := range sf.Commands {
		if c.Filename == "" {
			continue
		}
		switch c.Type {
		case "module", "assert_unlinkable", "assert_uninstantiable":
		default:
			continue
		}
		data, err := os.ReadFile(filepath.Join(tmp, c.Filename))
		if err != nil {
			t.Fatalf("read %s module %q: %v", base, c.Filename, err)
		}
		modules = append(modules, data)
	}
	return modules
}

func stagedCompactConsumerRoundTrip(t *testing.T, data []byte, wantImports []string) *Compiled {
	t.Helper()
	if _, err := Compile(nil, data); err == nil || !strings.Contains(err.Error(), "compact imports") {
		t.Fatalf("default compile error = %v, want fail-closed compact-import rejection", err)
	}
	compiled := stagedMultiMemoryCompile(t, data)
	if got := compiled.MemoryImports(); !equalStrings(got, wantImports) {
		t.Fatalf("compact memory imports = %v, want %v", got, wantImports)
	}
	blob, err := compiled.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal compact-import product: %v", err)
	}
	if err := compiled.Close(); err != nil {
		t.Fatalf("close source compact-import product: %v", err)
	}
	var public Compiled
	if err := public.UnmarshalBinary(blob); err == nil || !strings.Contains(err.Error(), "unknown required feature bits") {
		t.Fatalf("public compact-import product load error = %v, want fail-closed multi-memory rejection", err)
	}
	var loaded Compiled
	if err := unmarshalCompiled(&loaded, blob[5:]); err != nil {
		t.Fatalf("reload compact-import product: %v", err)
	}
	if got := loaded.MemoryImports(); !equalStrings(got, wantImports) {
		loaded.Close()
		t.Fatalf("reloaded compact memory imports = %v, want %v", got, wantImports)
	}
	// The staged execution bit is deliberately not serialized: public codec load
	// remains fail-closed until multi-memory is admitted. Reattach it explicitly
	// for this bounded internal execution proof after metadata was decoded.
	loaded.memoryDir.staged = true
	return &loaded
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func instantiateOfficialMemoryPair(t *testing.T, modules [][]byte) (*Instance, *Compiled, *Instance) {
	t.Helper()
	if len(modules) < 2 {
		t.Fatalf("official file emitted %d modules, want at least producer and consumer", len(modules))
	}
	producerCompiled := stagedMultiMemoryCompile(t, modules[0])
	producer, err := instantiateCore(producerCompiled, InstantiateOptions{})
	if err != nil {
		producerCompiled.Close()
		t.Fatalf("instantiate official producer: %v", err)
	}
	consumerCompiled := stagedCompactConsumerRoundTrip(t, modules[1], []string{"M.mem1", "M.mem2"})
	m1, err := producer.ExportedMemory("mem1")
	if err != nil {
		consumerCompiled.Close()
		producer.Close()
		producerCompiled.Close()
		t.Fatal(err)
	}
	m2, err := producer.ExportedMemory("mem2")
	if err != nil {
		consumerCompiled.Close()
		producer.Close()
		producerCompiled.Close()
		t.Fatal(err)
	}
	consumer, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{"M.mem1": m1, "M.mem2": m2}})
	if err != nil {
		consumerCompiled.Close()
		producer.Close()
		producerCompiled.Close()
		t.Fatalf("instantiate official compact consumer: %v", err)
	}
	return producer, producerCompiled, consumer
}

func TestStagedOfficialMultiMemoryCompactImportsExecute(t *testing.T) {
	t.Run("memory_size_import", func(t *testing.T) {
		modules := stagedOfficialMultiMemoryModules(t, "memory_size_import")
		if len(modules) != 2 {
			t.Fatalf("memory_size_import emitted %d modules, want 2", len(modules))
		}
		producer, producerCompiled, consumer := instantiateOfficialMemoryPair(t, modules)
		defer producerCompiled.Close()
		defer producer.Close()
		defer consumer.c.Close()
		defer consumer.Close()
		for i, want := range []int32{2, 0, 3, 4} {
			if got := tableTestCallI32(t, consumer, "size"+string(rune('1'+i))); got != want {
				t.Fatalf("official imported memory %d size = %d, want %d", i, got, want)
			}
		}
	})

	t.Run("memory_grow", func(t *testing.T) {
		modules := stagedOfficialMultiMemoryModules(t, "memory_grow")
		if len(modules) != 3 {
			t.Fatalf("memory_grow emitted %d modules, want 3", len(modules))
		}
		producer, producerCompiled, consumer := instantiateOfficialMemoryPair(t, modules)
		defer producerCompiled.Close()
		defer producer.Close()
		defer consumer.c.Close()
		defer consumer.Close()

		checkSizes := func(want ...int32) {
			t.Helper()
			for i := range want {
				if got := tableTestCallI32(t, consumer, "size"+string(rune('1'+i))); got != want[i] {
					t.Fatalf("official imported memory %d size = %d, want %d", i, got, want[i])
				}
			}
		}
		checkGrow := func(name string, delta, want int32) {
			t.Helper()
			if got := tableTestCallI32(t, consumer, name, I32(delta)); got != want {
				t.Fatalf("official %s(%d) = %d, want %d", name, delta, got, want)
			}
		}
		checkSizes(2, 0, 3, 4)
		checkGrow("grow1", 1, 2)
		checkSizes(3, 0, 3, 4)
		checkGrow("grow1", 2, 3)
		checkSizes(5, 0, 3, 4)
		checkGrow("grow1", 1, -1)
		checkSizes(5, 0, 3, 4)
		checkGrow("grow2", 10, 0)
		checkSizes(5, 10, 3, 4)
		checkGrow("grow3", 0x10000000, -1)
		checkSizes(5, 10, 3, 4)
		checkGrow("grow3", 3, 3)
		checkSizes(5, 10, 6, 4)
		checkGrow("grow4", 1, 4)
		checkGrow("grow4", 1, -1)
		checkSizes(5, 10, 6, 5)

		localCompiled := stagedMultiMemoryCompile(t, modules[2])
		defer localCompiled.Close()
		local, err := instantiateCore(localCompiled, InstantiateOptions{})
		if err != nil {
			t.Fatalf("instantiate official local grow module: %v", err)
		}
		defer local.Close()
		if got := tableTestCallI32(t, local, "size1"); got != 1 {
			t.Fatalf("local size1 = %d, want 1", got)
		}
		if got := tableTestCallI32(t, local, "size2"); got != 2 {
			t.Fatalf("local size2 = %d, want 2", got)
		}
		for _, tc := range []struct {
			name        string
			delta, want int32
		}{
			{name: "grow1", delta: 3, want: 1},
			{name: "grow1", delta: 4, want: 4},
			{name: "grow1", delta: 1, want: 8},
			{name: "grow2", delta: 1, want: 2},
			{name: "grow2", delta: 1, want: 3},
		} {
			if got := tableTestCallI32(t, local, tc.name, I32(tc.delta)); got != tc.want {
				t.Fatalf("local %s(%d) = %d, want %d", tc.name, tc.delta, got, tc.want)
			}
		}
	})
}
