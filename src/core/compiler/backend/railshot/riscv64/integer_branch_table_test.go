//go:build linux && !tinygo

package riscv64

import (
	"fmt"
	"testing"
)

func largeBranchTableFixture(count int, repeated bool) fixtureFunc {
	f := fixtureFunc{params: []byte{0x7f}, result: 0x7f,
		body: []byte{2, 0x7f, 2, 0x7f, 2, 0x7f, 0x41, 10, 0x20, 0, 0x0e}}
	f.body = uleb(f.body, uint32(count))
	for i := range count {
		label := byte(i & 1)
		if repeated {
			label = 0
		}
		f.body = append(f.body, label)
	}
	f.body = append(f.body, 2, 0x0b, 0x41, 1, 0x6a, 0x0b, 0x41, 2, 0x6a, 0x0b, 0x0b)
	return f
}

func TestLargeBranchTableAdmissionAndCodeSize(t *testing.T) {
	for _, count := range []int{maxTableEntries, maxTableEntries + 1, 24615, 32768} {
		for _, repeated := range []bool{false, true} {
			f := largeBranchTableFixture(count, repeated)
			c, err := CompileWithOptions(binaryModule(f), CompileOptions{ForceFallback: true})
			if err != nil {
				t.Fatal(count, repeated, err)
			}
			if count > maxTableEntries && c.functions[0].CodeBytes > count+512 {
				t.Fatal("large table duplicated executable transfers", count, c.functions[0].CodeBytes)
			}
			t.Logf("entries=%d repeated=%v code_bytes=%d", count, repeated, c.functions[0].CodeBytes)
		}
	}
	// Reject an impossible count before reading labels or allocating table data.
	for _, count := range []uint32{MaxModuleBytes, ^uint32(0), 1025} {
		f := fixtureFunc{result: 0x7f, body: uleb([]byte{0x41, 7, 0x41, 0, 0x0e}, count)}
		if c, err := CompileWithOptions(binaryModule(f), CompileOptions{ForceFallback: true}); c != nil || err == nil {
			t.Fatal("accepted truncated/over-bound table", count, c, err)
		}
	}
	// A bad label late in a large table must not bypass admission.
	f := largeBranchTableFixture(24615, false)
	f.body[len(f.body)-11] = 4
	if c, err := CompileWithOptions(binaryModule(f), CompileOptions{ForceFallback: true}); c != nil || err == nil {
		t.Fatal("accepted invalid large-table label", c, err)
	}
	f = fixtureFunc{result: 0x7f, body: excessiveTable(maxTableEntries + 1)}
	if _, err := CompileWithOptions(binaryModule(f), CompileOptions{ForceFallback: true}); err != nil {
		t.Fatal("repeated large function target", err)
	}
}

func TestNativeLargeBranchTable(t *testing.T) {
	for _, count := range []int{1025, 24615, 32768} {
		for _, repeated := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/%v", count, repeated), func(t *testing.T) {
				i := integerInstance(t, largeBranchTableFixture(count, repeated), true)
				for _, selector := range []uint64{0, 1, 2, 1023, 1024, uint64(count - 1), uint64(count), uint64(count + 1), 0x80000000, 0xffffffff, 0xdeadbeef00000000, 0xdeadbeef00000001} {
					want := uint64(10)
					if uint64(uint32(selector)) < uint64(count) {
						want = 13
						if !repeated && uint32(selector)&1 != 0 {
							want = 12
						}
					}
					expectInteger(t, i, want, 0, selector)
				}
				want := uint64(12)
				if repeated {
					want = 13
				}
				args := []uint64{1}
				if allocs := testing.AllocsPerRun(100, func() { expectInteger(t, i, want, 0, args...) }); allocs != 0 {
					t.Fatal("large-table steady-state allocations", allocs)
				}
			})
		}
	}
}

func TestLargeBranchTableMaximumLabel(t *testing.T) {
	f := fixtureFunc{params: []byte{0x7f}, result: 0x7f}
	for range maxControlNesting {
		f.body = append(f.body, 2, 0x7f)
	}
	f.body = append(f.body, 0x41, 7, 0x20, 0, 0x0e)
	f.body = uleb(f.body, maxTableEntries+1)
	for range maxTableEntries + 2 {
		f.body = uleb(f.body, maxControlNesting)
	}
	for range maxControlNesting + 1 {
		f.body = append(f.body, 0x0b)
	}
	if _, err := CompileWithOptions(binaryModule(f), CompileOptions{ForceFallback: true}); err != nil {
		t.Fatal(err)
	}
	i := integerInstance(t, f, true)
	for _, selector := range []uint64{0, 1024, 1025, 0xffffffff} {
		expectInteger(t, i, 7, 0, selector)
	}
}

func BenchmarkLargeBranchTableCompile(b *testing.B) {
	for _, count := range []int{1024, 1025, 24615, 32768} {
		data := binaryModule(largeBranchTableFixture(count, false))
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				c, err := CompileWithOptions(data, CompileOptions{ForceFallback: true})
				if err != nil {
					b.Fatal(err)
				}
				b.ReportMetric(float64(c.functions[0].CodeBytes), "code_bytes")
			}
		})
	}
}
