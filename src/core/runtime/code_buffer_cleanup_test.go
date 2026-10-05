//go:build (linux || darwin || windows) && (amd64 || arm64)

package runtime

import (
	"bytes"
	"errors"
	"testing"
)

func assertClosedCodeBuffer(t *testing.T, b *CodeBuffer) {
	t.Helper()
	if b.Bytes() != nil || b.Mapping() != nil || b.Base() != 0 {
		t.Fatal("failed owner exposes mapping")
	}
	if err := b.Append([]byte{1}); err == nil {
		t.Fatal("closed owner accepts writes")
	}
	if _, _, err := b.Take(); err == nil {
		t.Fatal("closed owner transfers mapping")
	}
	if err := b.Seal(); err == nil {
		t.Fatal("closed owner seals")
	}
}

func TestCodeBufferSealFailureRetainsCleanupOwnership(t *testing.T) {
	b, err := NewCodeBuffer(1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		b.ops = nil
		if err := b.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := b.Append([]byte{1, 2, 3, 4}); err != nil {
		t.Fatal(err)
	}
	original := b.mem
	sealFailure := errors.New("injected publication failure after RX")
	cleanupFailure := errors.New("injected seal rollback unmap failure")
	calls := 0
	b.ops = &codeBufferOps{
		seal: func(mem []byte) error {
			if err := protectCodeRX(mem); err != nil {
				return err
			}
			return sealFailure
		},
		unmap: func(mem []byte) error {
			calls++
			if calls == 1 {
				return cleanupFailure
			}
			return munmap(mem)
		},
	}
	if err := b.Seal(); !errors.Is(err, sealFailure) || !errors.Is(err, cleanupFailure) {
		t.Fatal(err)
	}
	if len(b.mem) != len(original) || &b.mem[0] != &original[0] || len(b.retired) != 0 {
		t.Fatal("seal lost mapping")
	}
	assertClosedCodeBuffer(t, b)
	if err := b.Close(); err != nil || b.mem != nil || calls != 2 {
		t.Fatal("cleanup retry", err, calls)
	}
	if err := b.Close(); err != nil || calls != 2 {
		t.Fatal("repeated cleanup", err, calls)
	}
}

func TestCodeBufferCloseFailureRetainsCleanupOwnership(t *testing.T) {
	b, err := NewCodeBuffer(1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		b.ops = nil
		if err := b.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := b.Append([]byte{1, 2, 3, 4}); err != nil {
		t.Fatal(err)
	}
	if err := b.Seal(); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("injected close unmap failure")
	calls := 0
	b.ops = &codeBufferOps{unmap: func(mem []byte) error {
		calls++
		if calls == 1 {
			return failure
		}
		return munmap(mem)
	}}
	if err := b.Close(); !errors.Is(err, failure) || len(b.mem) == 0 || b.registered {
		t.Fatal("close lost ownership", err)
	}
	assertClosedCodeBuffer(t, b)
	if err := b.Close(); err != nil || len(b.mem) != 0 || calls != 2 {
		t.Fatal("close retry", err, calls)
	}
}

func TestCodeBufferGrowthFailureRetainsBothMappings(t *testing.T) {
	for _, failures := range [][2]int{{2, 0}, {2, 1}, {1, 2}} {
		failRollback := failures[1] != 0
		b, err := NewCodeBuffer(1)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			b.ops = nil
			if err := b.Close(); err != nil {
				t.Error(err)
			}
		})
		if err := b.Append(bytes.Repeat([]byte{7}, len(b.mem))); err != nil {
			t.Fatal(err)
		}
		old := b.mem
		var replacement []byte
		oldCalls, newCalls := 0, 0
		retirementFailure := errors.New("injected old-image retirement failure")
		rollbackFailure := errors.New("injected replacement rollback failure")
		b.ops = &codeBufferOps{
			mapRW: func(n int) ([]byte, error) { var err error; replacement, err = mmapCodeRW(n); return replacement, err },
			unmap: func(mem []byte) error {
				if &mem[0] == &old[0] {
					oldCalls++
					if oldCalls <= failures[0] {
						return retirementFailure
					}
				} else {
					newCalls++
					if newCalls <= failures[1] {
						return rollbackFailure
					}
				}
				return munmap(mem)
			},
		}
		err = b.Append([]byte{9})
		if !errors.Is(err, retirementFailure) || errors.Is(err, rollbackFailure) != failRollback {
			t.Fatal(err)
		}
		if len(b.retired) == 0 || &b.retired[0] != &old[0] || (len(b.mem) != 0) != failRollback {
			t.Fatal("growth lost mapping", failRollback)
		}
		assertClosedCodeBuffer(t, b)
		if err := b.Close(); err == nil || (len(b.retired) != 0) != (failures[0] == 2) || (len(b.mem) != 0) != (failures[1] == 2) {
			t.Fatal("partial retry", failures, err)
		}
		if err := b.Close(); err != nil || b.retired != nil || b.mem != nil || oldCalls != failures[0]+1 {
			t.Fatal("final retry", err, oldCalls)
		}
		wantNewCalls := failures[1] + 1
		if newCalls != wantNewCalls {
			t.Fatal("replacement double release", newCalls, wantNewCalls)
		}
	}
}
