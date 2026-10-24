//go:build linux && !tinygo && (amd64 || arm64 || (riscv64 && !wago_target_tinygo))

package runtime

import (
	"bytes"
	"errors"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func compilerFileBuffer(t *testing.T) *CodeBuffer {
	t.Helper()
	b, err := NewCompilerCodeBuffer(compilerFileThreshold)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := b.Close(); err != nil {
			t.Error(err)
		}
	})
	if !b.OwnsCompilerFile() {
		t.Skip("disk-backed executable temporary storage unavailable")
	}
	return b
}

func TestCompilerFileReclaimPreservesBytes(t *testing.T) {
	b := compilerFileBuffer(t)
	want := make([]byte, 3*unix.Getpagesize()+19)
	for i := range want {
		want[i] = byte(i * 31)
	}
	if err := b.Append(want); err != nil {
		t.Fatal(err)
	}
	end, err := b.ReclaimCompilerPages(0, len(want))
	if err != nil {
		t.Fatal(err)
	}
	if end%unix.Getpagesize() != 0 {
		t.Fatal("reclaim boundary is not page aligned")
	}
	if err := b.FinishCompilerCode(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b.Bytes(), want) {
		t.Fatal("reclaimed compiler bytes changed")
	}
}

func TestCompilerFileTakeClosesHandleAndPreservesMapping(t *testing.T) {
	b := compilerFileBuffer(t)
	want := []byte{9, 8, 7, 6}
	if err := b.Append(want); err != nil {
		t.Fatal(err)
	}
	mem, _, err := b.Take()
	if err != nil {
		t.Fatal(err)
	}
	defer Unmap(mem)
	if b.OwnsCompilerFile() {
		t.Fatal("transfer retained staging descriptor")
	}
	if !bytes.Equal(mem[:len(want)], want) {
		t.Fatal("closed staging descriptor invalidated mapping")
	}
}

func TestCompilerFileSealClosesHandle(t *testing.T) {
	b := compilerFileBuffer(t)
	if err := b.Append([]byte{1, 2, 3, 4}); err != nil {
		t.Fatal(err)
	}
	if err := b.Seal(); err != nil {
		t.Fatal(err)
	}
	if b.OwnsCompilerFile() {
		t.Fatal("seal retained staging descriptor")
	}
	if !bytes.Equal(b.Bytes(), []byte{1, 2, 3, 4}) {
		t.Fatal("seal changed image")
	}
}

func TestCompilerFileFailedUnmapRetainsDescriptor(t *testing.T) {
	b := compilerFileBuffer(t)
	marker := errors.New("unmap failed")
	b.ops.unmap = func([]byte) error { return marker }
	if err := b.Close(); !errors.Is(err, marker) {
		t.Fatal(err)
	}
	if !b.OwnsCompilerFile() || b.mem == nil {
		t.Fatal("failed unmap lost owned resources")
	}
	b.ops.unmap = munmap
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	if b.OwnsCompilerFile() {
		t.Fatal("cleanup retained staging descriptor")
	}
}

func TestCompilerFileUnavailableDirectoryFallsBack(t *testing.T) {
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
	b, err := NewCompilerCodeBuffer(compilerFileThreshold)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if b.OwnsCompilerFile() {
		t.Fatal("unexpected staging file")
	}
	if err := b.Append([]byte{1}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b.Bytes(), []byte{1}) {
		t.Fatal("anonymous fallback changed bytes")
	}
}

func TestCompilerFileReservationFailureClosesDescriptor(t *testing.T) {
	owner := &compilerImageOwner{}
	fd := -1
	owner.reserve = func(open int, _ int64) error { fd = open; return unix.ENOSPC }
	mem, err := owner.allocate(compilerFileThreshold)
	if err != nil {
		t.Fatal(err)
	}
	defer munmap(mem)
	if fd < 0 {
		t.Skip("temporary disk-backed file unavailable")
	}
	if len(owner.files) != 0 {
		t.Fatal("failed reservation retained image")
	}
	if _, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0); !errors.Is(err, unix.EBADF) {
		t.Fatalf("descriptor still open: %v", err)
	}
}

func TestCompilerFileGrowthRetiresOldHandle(t *testing.T) {
	b := compilerFileBuffer(t)
	old := b.mem
	want := bytes.Repeat([]byte{0x5a}, len(old)+17)
	if err := b.Append(want); err != nil {
		t.Fatal(err)
	}
	if b.ops.hasFile(old) {
		t.Fatal("growth retained the retired staging handle")
	}
	if !b.OwnsCompilerFile() || compilerImageKey(b.mem) == compilerImageKey(old) {
		t.Fatal("growth did not replace the file mapping")
	}
	if err := b.FinishCompilerCode(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b.Bytes(), want) {
		t.Fatal("growth changed compiler bytes")
	}
}

func TestCompilerFileIndependentOwners(t *testing.T) {
	for i := 0; i < 4; i++ {
		t.Run(string(rune('A'+i)), func(t *testing.T) {
			t.Parallel()
			b := compilerFileBuffer(t)
			want := bytes.Repeat([]byte{byte(i + 1)}, 2*unix.Getpagesize()+7)
			if err := b.Append(want); err != nil {
				t.Fatal(err)
			}
			if _, err := b.ReclaimCompilerPages(0, len(want)); err != nil {
				t.Fatal(err)
			}
			if err := b.FinishCompilerCode(); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(b.Bytes(), want) {
				t.Fatal("independent compiler image changed")
			}
		})
	}
}
