//go:build linux && amd64

package wago

import (
	"bytes"
	"testing"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

// Use the ordinary lazy memory mapping and Go accessors only. No WebAssembly
// or generated native code is compiled or executed by this fixture.
func readTestInstance(t testing.TB, size int) *Instance {
	t.Helper()
	jm, err := coreruntime.NewJobMemoryGrowable(size, size)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := jm.Close(); err != nil {
			t.Error(err)
		}
	})
	return &Instance{c: &Compiled{}, jm: jm}
}

func TestInstanceReadMemory32Boundary(t *testing.T) {
	const size = 1 << 32
	in := readTestInstance(t, size)
	want := []byte{1, 2, 3, 4}
	copy(in.jm.HostBytes()[size-4:], want)
	for _, tc := range []struct {
		name           string
		offset, length uint32
		want           []byte
		ok             bool
	}{
		{"below-end", size - 4, 3, want[:3], true},
		{"at-end", size - 4, 4, want, true},
		{"last-byte", size - 1, 1, want[3:], true},
		{"past-end", size - 1, 2, nil, false},
		{"empty", size - 1, 0, []byte{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := in.Read(tc.offset, tc.length)
			if ok != tc.ok || !bytes.Equal(got, tc.want) {
				t.Fatalf("Read = %v, %v; want %v, %v", got, ok, tc.want, tc.ok)
			}
			if len(got) > 0 {
				got[0] ^= 0xff
				if in.jm.HostBytes()[tc.offset] == got[0] {
					t.Fatal("Read returned an alias instead of a copy")
				}
			}
		})
	}
}

func TestInstanceReadEmptyEnd(t *testing.T) {
	in := readTestInstance(t, 65536)
	if got, ok := in.Read(65536, 0); !ok || len(got) != 0 {
		t.Fatalf("empty read at end = %v, %v", got, ok)
	}
	if _, ok := in.Read(65537, 0); ok {
		t.Fatal("empty read beyond end accepted")
	}
	var absent *Instance
	if _, ok := absent.Read(0, 0); ok {
		t.Fatal("nil instance read accepted")
	}
}

func BenchmarkInstanceReadCopy(b *testing.B) {
	in := readTestInstance(b, 65536)
	b.ReportAllocs()
	b.SetBytes(32)
	b.ResetTimer()
	for range b.N {
		got, ok := in.Read(16, 32)
		if !ok || len(got) != 32 {
			b.Fatal("read failed")
		}
	}
}
