//go:build amd64

package amd64

import (
	"testing"

	encoderamd64 "github.com/wago-org/wago/src/core/encoder/amd64"
)

func TestConstPoolUsesFlatReusableStorageAMD64(t *testing.T) {
	f := fn{
		transient: transient{
			v128Pool:  make([]poolConst, 0, 2),
			poolSites: make([]poolSite, 0, 3),
		},
	}
	a := [16]byte{1, 2, 3, 4}
	b := [16]byte{1, 2, 3, 4, 5}
	allocs := testing.AllocsPerRun(100, func() {
		f.v128Pool = f.v128Pool[:0]
		f.poolSites = f.poolSites[:0]
		f.recordConst(a[:4], 11)
		f.recordConst(b[:8], 22)
		f.recordConst(a[:4], 33)
	})
	if allocs != 0 {
		t.Fatalf("recording into retained pool storage allocated %.1f times", allocs)
	}
	if got := len(f.v128Pool); got != 2 {
		t.Fatalf("distinct constants = %d, want 2", got)
	}
	if got := len(f.poolSites); got != 3 {
		t.Fatalf("flat sites = %d, want 3", got)
	}
	first := f.v128Pool[0]
	if first.size != 4 || first.head == 0 {
		t.Fatalf("first constant = %#v", first)
	}
	latest := f.poolSites[first.head-1]
	older := f.poolSites[latest.next-1]
	if latest.off != 33 || older.off != 11 || older.next != 0 {
		t.Fatalf("site chain = %#v -> %#v, want 33 -> 11", latest, older)
	}
}

func TestConstPoolAttributesLiteralBytesAMD64(t *testing.T) {
	f := fn{a: &encoderamd64.Asm{B: make([]byte, 4)}, stats: &CodegenStats{}}
	f.recordConst([]byte{1, 2, 3, 4}, 0)
	f.emitV128ConstPool()
	if got := f.stats.NativeSize.LiteralPoolBytes; got != 4 {
		t.Fatalf("literal bytes = %d, want 4", got)
	}
	if got := len(f.a.B); got != 8 {
		t.Fatalf("code plus pool bytes = %d, want 8", got)
	}
}
