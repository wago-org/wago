//go:build linux

package wagobench

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	core "github.com/wago-org/wago/src/core/runtime"
)

var benchMappingSequence atomic.Uint64

// An address can be reused as soon as Close unmaps it. A unique VMA name
// identifies the owned mapping even when another allocation reuses its range.
func benchNameMapping(owner *core.CodeBuffer) (string, error) {
	const prSetVMA = 0x53564d41
	const prSetVMAAnonName = 0
	name := fmt.Sprintf("wago-bench-%d-%d", os.Getpid(), benchMappingSequence.Add(1))
	text := append([]byte(name), 0)
	_, _, errno := syscall.Syscall6(syscall.SYS_PRCTL, prSetVMA, prSetVMAAnonName, owner.Base(), uintptr(len(owner.Mapping())), uintptr(unsafe.Pointer(&text[0])), 0)
	runtime.KeepAlive(text)
	runtime.KeepAlive(owner)
	if errno != 0 {
		return "", errno
	}
	return name, nil
}

func benchRequireMappingName(t *testing.T, owner *core.CodeBuffer) string {
	t.Helper()
	name, err := benchNameMapping(owner)
	if errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENOSYS) || errors.Is(err, syscall.EOPNOTSUPP) || errors.Is(err, syscall.EPERM) {
		if os.Getenv("WAGO_REQUIRE_MAPPING_IDENTITY") == "1" {
			t.Fatalf("required OS mapping identity unavailable: %v", err)
		}
		t.Skipf("OS mapping identity not qualified: anonymous VMA naming unavailable: %v", err)
	}
	if err != nil {
		t.Fatalf("name owned mapping: %v", err)
	}
	return name
}

func benchMappingHasName(line, name string) bool {
	return strings.HasSuffix(strings.TrimSpace(line), "[anon:"+name+"]")
}

func benchNamedMappings(t *testing.T, name string) []string {
	t.Helper()
	f, err := os.Open("/proc/self/maps")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var found []string
	s := bufio.NewScanner(f)
	for s.Scan() {
		if benchMappingHasName(s.Text(), name) {
			found = append(found, s.Text())
		}
	}
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
	return found
}

func benchAssertClosedOwner(t *testing.T, cm *benchCompiledModule, owner *core.CodeBuffer) {
	t.Helper()
	if err := cm.Close(); err != nil {
		t.Fatalf("Close owned mapping: %v", err)
	}
	if cm.Code != nil || cm.Entry != nil || cm.image != nil || owner.Base() != 0 || owner.Bytes() != nil || owner.Mapping() != nil {
		t.Fatal("Close retained a compiled-output or owner view")
	}
	if _, _, err := owner.Take(); err == nil {
		t.Fatal("closed owner accepted Take")
	}
	if err := owner.Close(); err != nil {
		t.Fatalf("repeat Close: %v", err)
	}
}

func TestBenchNativeMappingRelease(t *testing.T) {
	m, err := wasm.DecodeModule(fibWasm)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	for _, identity := range []bool{false, true} {
		name := "owner_state"
		if identity {
			name = "mapping_identity"
		}
		t.Run(name, func(t *testing.T) {
			for i := 0; i < 128; i++ {
				cm, err := benchCompileModule(m)
				if err != nil {
					t.Fatal(err)
				}
				// Also release the first output when the OS identity subtest skips.
				t.Cleanup(func() {
					if err := cm.Close(); err != nil {
						t.Error(err)
					}
				})
				owner, ok := cm.image.(*core.CodeBuffer)
				if !ok {
					t.Fatal("missing native owner")
				}
				if owner.Base() == 0 || len(owner.Mapping()) == 0 {
					t.Fatalf("iteration %d: missing live mapping before Close", i)
				}
				var token string
				if identity {
					token = benchRequireMappingName(t, owner)
					if maps := benchNamedMappings(t, token); len(maps) == 0 {
						t.Fatalf("iteration %d: owned mapping %s missing before Close (base=%#x capacity=%d)", i, token, owner.Base(), len(owner.Mapping()))
					}
				}
				benchAssertClosedOwner(t, cm, owner)
				if identity {
					if maps := benchNamedMappings(t, token); len(maps) != 0 {
						t.Fatalf("iteration %d: owned mapping %s retained after Close: %v", i, token, maps)
					}
				}
			}
		})
	}
}

func TestBenchMappingIdentityPredicate(t *testing.T) {
	const retained = "1000-2000 r-xp 00000000 00:00 0 [anon:original]"
	const reused = "1000-2000 r-xp 00000000 00:00 0 [anon:replacement]"
	if !benchMappingHasName(retained, "original") {
		t.Fatal("retained original not detected")
	}
	if benchMappingHasName(reused, "original") {
		t.Fatal("replacement at reused range mistaken for original")
	}
	if benchMappingHasName(retained, "orig") {
		t.Fatal("partial name matched")
	}
}

func TestBenchMappingIdentityAddressReuse(t *testing.T) {
	first, err := core.NewCodeBuffer(os.Getpagesize())
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	token := benchRequireMappingName(t, first)
	addr, size := first.Base(), len(first.Mapping())
	// The predicate must detect a deliberately retained original allocation.
	if len(benchNamedMappings(t, token)) == 0 {
		t.Fatal("retained original mapping not detected")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if len(benchNamedMappings(t, token)) != 0 {
		t.Fatal("original identity remained after Close")
	}
	for attempt := 0; attempt < 64; attempt++ {
		replacement, err := core.NewCodeBuffer(size)
		if err != nil {
			t.Fatal(err)
		}
		if replacement.Base() != addr {
			if err := replacement.Close(); err != nil {
				t.Fatal(err)
			}
			continue
		}
		defer replacement.Close()
		other := benchRequireMappingName(t, replacement)
		if len(benchNamedMappings(t, other)) == 0 {
			t.Fatal("replacement mapping not detected")
		}
		if maps := benchNamedMappings(t, token); len(maps) != 0 {
			t.Fatalf("address reuse retained original identity: %v", maps)
		}
		return
	}
	if os.Getenv("WAGO_REQUIRE_MAPPING_IDENTITY") == "1" {
		t.Fatal("required OS mapping identity reuse case could not obtain the released address")
	}
	t.Skip("OS mapping identity reuse case not qualified: released address was not reused")
}
