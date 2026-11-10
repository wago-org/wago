//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"context"
	"errors"
	"math/rand"
	"runtime"
	"testing"

	native "github.com/wago-org/wago/src/core/runtime/riscv64"
)

type bulkDataFixture struct {
	passive bool
	offset  uint32
	bytes   []byte
}

func withBulkData(b []byte, data []bulkDataFixture, count bool) []byte {
	if count {
		b = modifySection(b, 10, 12, uleb(nil, uint32(len(data))), false)
	}
	d := uleb(nil, uint32(len(data)))
	for _, s := range data {
		if s.passive {
			d = append(d, 1)
		} else {
			d = append(d, 0, 0x41)
			d = sleb(d, int64(int32(s.offset)))
			d = append(d, 0x0b)
		}
		d = uleb(d, uint32(len(s.bytes)))
		d = append(d, s.bytes...)
	}
	return section(b, 11, d)
}

func bulkFixture(sub byte, index uint32) fixtureFunc {
	f := fixtureFunc{result: 0x7f}
	if sub != 9 {
		f.params = []byte{0x7f, 0x7f, 0x7f}
		f.body = []byte{0x20, 0, 0x20, 1, 0x20, 2}
	}
	f.body = append(f.body, 0xfc, sub)
	switch sub {
	case 8:
		f.body = uleb(f.body, index)
		f.body = append(f.body, 0)
	case 9:
		f.body = uleb(f.body, index)
	case 10:
		f.body = append(f.body, 0, 0)
	case 11:
		f.body = append(f.body, 0)
	}
	f.body = append(f.body, 0x41, 37, 0x0b)
	return f
}
func bulkInstance(t *testing.T, fs []fixtureFunc, data []bulkDataFixture) *Instance {
	t.Helper()
	b := withBulkData(stateModule(fs, false, 1, 2, true, nil, nil), data, true)
	return stateInstance(t, b, InstantiateOptions{})
}
func checkBulkCall(t *testing.T, i *Instance, name string, trap bool, args ...uint64) {
	t.Helper()
	v, err := i.Invoke(context.Background(), name, args...)
	if trap {
		var te *native.TrapError
		if !errors.As(err, &te) || te.Code != TrapMemoryBounds {
			t.Fatal("expected bulk bounds trap", v, err)
		}
	} else if err != nil || v != 37 {
		t.Fatal("bulk success", v, err)
	}
}
func readBulkMemory(t *testing.T, i *Instance, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if err := i.ReadMemory("mem", 0, b); err != nil {
		t.Fatal(err)
	}
	return b
}
func dirty(v uint32) uint64 { return uint64(v) | 0xdeadbeef00000000 }

func TestNativeBulkCopyOverlapBoundsAndReuse(t *testing.T) {
	i := bulkInstance(t, []fixtureFunc{bulkFixture(10, 0)}, nil)
	initial := make([]byte, 65536)
	for j := range initial {
		initial[j] = byte(j*29 + j/251)
	}
	cases := [][3]uint32{{3, 0, 64}, {0, 3, 64}, {10, 10, 30}, {3, 80, 17}, {80, 3, 17}, {65520, 65528, 8}, {65528, 65520, 8}, {0, 0, 65536}, {65536, 65536, 0}}
	rng := rand.New(rand.NewSource(80204))
	for j := 0; j < 64; j++ {
		cases = append(cases, [3]uint32{uint32(rng.Intn(192)), uint32(rng.Intn(192)), uint32(rng.Intn(65))})
	}
	for _, c := range cases {
		want := append([]byte(nil), initial...)
		copy(want[c[0]:c[0]+c[2]], want[c[1]:c[1]+c[2]])
		if err := i.WriteMemory("mem", 0, initial); err != nil {
			t.Fatal(err)
		}
		checkBulkCall(t, i, "a", false, dirty(c[0]), dirty(c[1]), dirty(c[2]))
		if got := readBulkMemory(t, i, len(want)); !bytes.Equal(got, want) {
			t.Fatal("memmove mismatch or neighboring write", c)
		}
	}
	for _, c := range [][3]uint32{{65536, 0, 1}, {0, 65536, 1}, {65535, 0, 2}, {0, 65535, 2}, {65537, 0, 0}, {0, 65537, 0}, {65537, 65537, 0}, {65535, 65535, 2}, {0xffffffff, 0, 0}, {0, 0, 0xffffffff}, {65535, 65535, 0xffffffff}} {
		before := readBulkMemory(t, i, len(initial))
		checkBulkCall(t, i, "a", true, dirty(c[0]), dirty(c[1]), dirty(c[2]))
		if !bytes.Equal(before, readBulkMemory(t, i, len(initial))) {
			t.Fatal("copy mutated memory before full preflight", c)
		}
		checkBulkCall(t, i, "a", false, 65536, 65536, 0)
	}
}

func TestNativeBulkFillByteWidthBoundsAndReuse(t *testing.T) {
	i := bulkInstance(t, []fixtureFunc{bulkFixture(11, 0)}, nil)
	for _, c := range [][3]uint32{{3, 0xffffffff, 17}, {1, 0x80ff, 1}, {8, 0x12348000, 64}, {65535, 0x180, 1}, {65536, 0xab, 0}, {0, 0x5a, 65536}} {
		initial := bytes.Repeat([]byte{0x31}, 65536)
		want := append([]byte(nil), initial...)
		for j := c[0]; j < c[0]+c[2]; j++ {
			want[j] = byte(c[1])
		}
		if err := i.WriteMemory("mem", 0, initial); err != nil {
			t.Fatal(err)
		}
		checkBulkCall(t, i, "a", false, dirty(c[0]), dirty(c[1]), dirty(c[2]))
		if !bytes.Equal(readBulkMemory(t, i, len(want)), want) {
			t.Fatal("fill byte truncation or neighboring write", c)
		}
	}
	for _, c := range [][2]uint32{{65536, 1}, {65535, 2}, {65537, 0}, {0xffffffff, 0}, {0, 0xffffffff}, {0xffffffff, 0xffffffff}} {
		before := readBulkMemory(t, i, 65536)
		checkBulkCall(t, i, "a", true, dirty(c[0]), dirty(99), dirty(c[1]))
		if !bytes.Equal(before, readBulkMemory(t, i, 65536)) {
			t.Fatal("partial fill before bounds trap", c)
		}
		checkBulkCall(t, i, "a", false, 65536, 0, 0)
	}
}

func TestNativeBulkInitActiveEmptyDropAndIsolation(t *testing.T) {
	data := []bulkDataFixture{{offset: 20, bytes: []byte{7, 8}}, {passive: true, bytes: []byte{3, 5, 7, 11, 13, 17, 19}}, {passive: true}, {passive: true, bytes: []byte{23, 29}}}
	fs := []fixtureFunc{bulkFixture(8, 1), bulkFixture(9, 1), bulkFixture(8, 0), bulkFixture(8, 2), bulkFixture(8, 3)}
	b := withBulkData(stateModule(fs, true, 1, 2, true, nil, nil), data, true)
	c, err := Compile(b)
	if err != nil {
		t.Fatal(err)
	}
	clear(b) // Compiled passive and active bytes must own the source.
	if runtime.GOARCH != "riscv64" {
		t.Skip("RV64 execution only; owned compilation/admission passed")
	}
	m, err := native.NewLinearMemory(1, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	opts := InstantiateOptions{ImportedMemory: m, ImportModule: "m", ImportName: "x"}
	newInstance := func() *Instance {
		i, err := c.InstantiateWithOptions(native.MinNativeStackBytes, opts)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := i.Close(); err != nil {
				t.Error(err)
			}
		})
		return i
	}
	a, other := newInstance(), newInstance()
	if got := readBulkMemory(t, a, 22); got[20] != 7 || got[21] != 8 || got[0] != 0 {
		t.Fatal("active/passive instantiation semantics", got)
	}
	for _, call := range []struct{ dst, src, n uint32 }{{3, 0, 7}, {65535, 6, 1}, {65536, 7, 0}, {9, 2, 3}} {
		before := readBulkMemory(t, a, 65536)
		want := append([]byte(nil), before...)
		copy(want[call.dst:call.dst+call.n], data[1].bytes[call.src:call.src+call.n])
		checkBulkCall(t, a, "a", false, dirty(call.dst), dirty(call.src), dirty(call.n))
		if !bytes.Equal(readBulkMemory(t, a, len(want)), want) {
			t.Fatal("init mismatch or neighboring write", call)
		}
	}
	for _, call := range [][3]uint32{{65535, 0, 2}, {0, 6, 2}, {0, 8, 0}, {65537, 0, 0}, {0xffffffff, 0, 1}, {0, 0xffffffff, 1}, {0, 0, 0xffffffff}} {
		before := readBulkMemory(t, a, 65536)
		checkBulkCall(t, a, "a", true, dirty(call[0]), dirty(call[1]), dirty(call[2]))
		if !bytes.Equal(before, readBulkMemory(t, a, 65536)) {
			t.Fatal("init partial mutation", call)
		}
	}
	for _, name := range []string{"c", "d"} {
		checkBulkCall(t, a, name, false, 65536, 0, 0)
		checkBulkCall(t, a, name, true, 0, 0, 1)
		checkBulkCall(t, a, name, true, 0, 1, 0)
	}
	checkBulkCall(t, a, "e", false, 30, 0, 2) // segment index preserved across active/empty entries
	checkBulkCall(t, a, "b", false)
	checkBulkCall(t, a, "b", false)
	checkBulkCall(t, a, "a", false, 65536, 0, 0)
	for _, args := range [][]uint64{{0, 0, 1}, {0, 1, 0}, {65537, 0, 0}} {
		before := readBulkMemory(t, a, 65536)
		checkBulkCall(t, a, "a", true, args...)
		if !bytes.Equal(before, readBulkMemory(t, a, 65536)) {
			t.Fatal("dropped init mutation")
		}
	}
	checkBulkCall(t, other, "a", false, 40, 0, 7)
	if got := readBulkMemory(t, other, 47); !bytes.Equal(got[40:], data[1].bytes) {
		t.Fatal("drop crossed instance or source ownership lost")
	}
}

func TestNativeBulkTrapInstructionOrdering(t *testing.T) {
	// A successful fill remains when the next init traps. Later fill/drop must
	// not execute, while drop reached before a trap must remain effective.
	f := fixtureFunc{result: 0x7f, body: []byte{0x41, 4, 0x41, 33, 0x41, 4, 0xfc, 11, 0, 0x41, 0, 0x41, 9, 0x41, 1, 0xfc, 8, 0, 0, 0x41, 20, 0x41, 55, 0x41, 1, 0xfc, 11, 0, 0xfc, 9, 0, 0x41, 37, 0x0b}}
	dropThenTrap := fixtureFunc{result: 0x7f, body: []byte{0xfc, 9, 0, 0x41, 0, 0x41, 0, 0x41, 1, 0xfc, 8, 0, 0, 0x41, 37, 0x0b}}
	i := bulkInstance(t, []fixtureFunc{f, bulkFixture(8, 0), dropThenTrap}, []bulkDataFixture{{passive: true, bytes: []byte{3, 5}}})
	checkBulkCall(t, i, "a", true)
	got := readBulkMemory(t, i, 24)
	want := make([]byte, 24)
	copy(want[4:8], []byte{33, 33, 33, 33})
	if !bytes.Equal(got, want) {
		t.Fatal("trap reordered instruction effects", got, want)
	}
	checkBulkCall(t, i, "b", false, 10, 0, 2)
	checkBulkCall(t, i, "c", true)
	checkBulkCall(t, i, "b", true, 10, 0, 1)
	checkBulkCall(t, i, "b", false, 65536, 0, 0)
}

func TestNativeBulkPassiveOnlyDrop(t *testing.T) {
	b := withBulkData(binaryModule(bulkFixture(9, 0)), []bulkDataFixture{{passive: true, bytes: []byte{1, 2, 3}}}, true)
	i := stateInstance(t, b, InstantiateOptions{})
	if i.memory != nil || i.lease != nil || i.context == nil {
		t.Fatal("passive-only context ownership")
	}
	checkBulkCall(t, i, "a", false)
	checkBulkCall(t, i, "a", false)
}

func TestNativeBulkZeroMemoryAndGrowth(t *testing.T) {
	fs := []fixtureFunc{bulkFixture(8, 0), bulkFixture(10, 0), bulkFixture(11, 0), growFixture()}
	data := []bulkDataFixture{{passive: true, bytes: []byte{17, 19, 23}}}
	b := withBulkData(stateModule(fs, false, 0, 2, true, nil, nil), data, true)
	i := stateInstance(t, b, InstantiateOptions{})
	for _, name := range []string{"a", "b", "c"} {
		checkBulkCall(t, i, name, false, dirty(0), dirty(0), dirty(0))
		checkBulkCall(t, i, name, true, dirty(1), dirty(0), dirty(0))
		checkBulkCall(t, i, name, true, dirty(0), dirty(0), dirty(1))
	}
	// Source bounds remain meaningful when no destination bytes are requested.
	checkBulkCall(t, i, "a", false, 0, 3, 0)
	checkBulkCall(t, i, "a", true, 0, 4, 0)
	checkBulkCall(t, i, "b", true, 0, 1, 0)
	if old, err := i.Invoke(context.Background(), "d", dirty(2)); err != nil || old != 0 {
		t.Fatal("growth before bulk", old, err)
	}
	checkBulkCall(t, i, "a", false, 131069, 0, 3)
	checkBulkCall(t, i, "b", false, 65535, 131069, 3)
	checkBulkCall(t, i, "c", false, 65537, dirty(0x123480ff), 1)
	got := readBulkMemory(t, i, 131072)
	if !bytes.Equal(got[131069:], []byte{17, 19, 23}) || !bytes.Equal(got[65535:65538], []byte{17, 19, 255}) {
		t.Fatal("bulk used stale pre-growth base/length or wrong byte value")
	}
	checkBulkCall(t, i, "a", true, 131070, 0, 3)
	checkBulkCall(t, i, "b", false, 131072, 131072, 0)
	checkBulkCall(t, i, "c", false, 131072, 0, 0)
}

func TestNativeBulkLargeDescriptorAndAllocations(t *testing.T) {
	data := make([]bulkDataFixture, 256)
	for j := range data {
		data[j].passive = true
	}
	data[255].bytes = []byte{17, 19}
	globals := make([]globalFixture, 256)
	for j := range globals {
		globals[j] = globalFixture{wide: true, mutable: true, value: int64(j)}
	}
	fs := []fixtureFunc{bulkFixture(8, 255), bulkFixture(9, 255), bulkFixture(10, 0), bulkFixture(11, 0)}
	b := withBulkData(stateModule(fs, false, 1, 2, true, globals, nil), data, true)
	i := stateInstance(t, b, InstantiateOptions{})
	args := []uint64{3, 0, 2}
	checkBulkCall(t, i, "a", false, args...)
	if got := readBulkMemory(t, i, 5); !bytes.Equal(got[3:], []byte{17, 19}) {
		t.Fatal("large data descriptor offset", got)
	}
	if got, err := i.Global("g255"); err != nil || got != 255 {
		t.Fatal("bulk clobbered globals", got, err)
	}
	for _, name := range []string{"a", "c", "d"} {
		var bad bool
		allocs := testing.AllocsPerRun(50, func() { v, err := i.Invoke(context.Background(), name, args...); bad = bad || err != nil || v != 37 })
		if bad || allocs != 0 {
			t.Fatal("steady-state bulk allocation/result", name, allocs, bad)
		}
	}
	checkBulkCall(t, i, "b", false)
	checkBulkCall(t, i, "a", true, args...)
}

func TestBulkDataCountAndUnsupportedPrefixAdmission(t *testing.T) {
	base := stateModule([]fixtureFunc{bulkFixture(8, 0)}, false, 1, 2, true, nil, nil)
	data := []bulkDataFixture{{passive: true, bytes: []byte{1}}}
	good := withBulkData(base, data, true)
	if c, err := Compile(good); err != nil || c == nil {
		t.Fatal("valid data count", err)
	}
	for _, b := range [][]byte{withBulkData(base, data, false), modifySection(good, 12, 12, []byte{0}, true), modifySection(good, 12, 12, []byte{2}, true)} {
		if c, err := Compile(b); c != nil || err == nil || errors.Is(err, ErrUnsupported) {
			t.Fatal("invalid data count bypassed normal validation", c, err)
		}
	}
	empty := withBulkData(binaryModule(fixtureFunc{result: 0x7f, body: []byte{0x41, 0, 0x0b}}), nil, true)
	if _, err := Compile(empty); err != nil {
		t.Fatal("consistent zero count", err)
	}
	bad := fixtureFunc{result: 0x7f, body: []byte{0, 0xfc, 0, 0x0b}, unexported: true}
	if c, err := Compile(binaryModule(fixtureFunc{result: 0x7f, body: []byte{0x41, 0, 0x0b}}, bad)); c != nil || !errors.Is(err, ErrUnsupported) {
		t.Fatal("hidden unsupported prefix admitted", c, err)
	}
}
