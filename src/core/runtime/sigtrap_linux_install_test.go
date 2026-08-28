//go:build linux && (amd64 || arm64) && wago_guardpage

package runtime

import (
	"errors"
	"syscall"
	"testing"
)

func TestInstallLinuxSignalHandlersPublishesDistinctChainTargets(t *testing.T) {
	guardOldSEGVHandler = 0
	guardOldBUSHandler = 0
	act := kernelSigaction{handler: 0x9000}
	calls := 0
	err := installLinuxSignalHandlers(&act, func(sig uintptr, installed, old *kernelSigaction) error {
		calls++
		switch calls {
		case 1:
			if sig != uintptr(syscall.SIGSEGV) || installed != nil || old == nil {
				t.Fatalf("read SIGSEGV call = sig %d act %p old %p", sig, installed, old)
			}
			old.handler = 0x1111
			old.mask = 0x11
			old.flags = _SA_RESTART
		case 2:
			if sig != uintptr(syscall.SIGBUS) || installed != nil || old == nil {
				t.Fatalf("read SIGBUS call = sig %d act %p old %p", sig, installed, old)
			}
			old.handler = 0x2222
			old.mask = 0x22
			old.flags = _SA_NODEFER
		case 3, 4:
			if installed == nil || old != nil || installed == &act || installed.handler != act.handler {
				t.Fatalf("install call %d = act %p old %p", calls, installed, old)
			}
			wantMask, wantFlag := uint64(0x11), uint64(_SA_RESTART)
			if calls == 4 {
				wantMask, wantFlag = 0x22, _SA_NODEFER
			}
			if installed.mask != wantMask || installed.flags&wantFlag == 0 {
				t.Fatalf("install call %d lost prior mask/flags: %+v", calls, installed)
			}
			if guardOldSEGVHandler != 0x1111 || guardOldBUSHandler != 0x2222 {
				t.Fatalf("chain targets were not published before install: %#x/%#x", guardOldSEGVHandler, guardOldBUSHandler)
			}
		default:
			t.Fatalf("unexpected rt_sigaction call %d", calls)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 4 || guardOldSEGVHandler != 0x1111 || guardOldBUSHandler != 0x2222 {
		t.Fatalf("calls/targets = %d %#x/%#x", calls, guardOldSEGVHandler, guardOldBUSHandler)
	}
}

func TestInstallLinuxSignalHandlersRollsBackSIGSEGV(t *testing.T) {
	guardOldSEGVHandler = 0
	guardOldBUSHandler = 0
	act := kernelSigaction{handler: 0x9000}
	calls := 0
	busErr := errors.New("bus install failed")
	err := installLinuxSignalHandlers(&act, func(sig uintptr, installed, old *kernelSigaction) error {
		calls++
		switch calls {
		case 1:
			old.handler = 0x1111
			old.flags = 0x12
		case 2:
			old.handler = 0x2222
			old.flags = 0x34
		case 3:
			if sig != uintptr(syscall.SIGSEGV) || installed == nil || installed.handler != act.handler {
				t.Fatalf("SIGSEGV install = sig %d act %p", sig, installed)
			}
		case 4:
			if sig != uintptr(syscall.SIGBUS) || installed == nil || installed.handler != act.handler {
				t.Fatalf("SIGBUS install = sig %d act %p", sig, installed)
			}
			return busErr
		case 5:
			if sig != uintptr(syscall.SIGSEGV) || installed == nil || installed.handler != 0x1111 || installed.flags != 0x12 {
				t.Fatalf("SIGSEGV rollback = sig %d act %+v", sig, installed)
			}
		default:
			t.Fatalf("unexpected rt_sigaction call %d", calls)
		}
		return nil
	})
	if !errors.Is(err, busErr) {
		t.Fatalf("install error = %v, want %v", err, busErr)
	}
	if calls != 5 || guardOldSEGVHandler != 0 || guardOldBUSHandler != 0 {
		t.Fatalf("rollback calls/targets = %d %#x/%#x", calls, guardOldSEGVHandler, guardOldBUSHandler)
	}
}

func TestInstallLinuxSignalHandlersRejectsOneShotPredecessor(t *testing.T) {
	act := kernelSigaction{handler: 0x9000}
	err := installLinuxSignalHandlers(&act, func(sig uintptr, _ *kernelSigaction, old *kernelSigaction) error {
		old.handler = 0x1111
		if sig == uintptr(syscall.SIGSEGV) {
			old.flags = _SA_RESETHAND
		}
		return nil
	})
	if err == nil {
		t.Fatal("one-shot predecessor was accepted")
	}
}

func TestInstallLinuxSignalHandlersRejectsUnchainableDisposition(t *testing.T) {
	act := kernelSigaction{handler: 0x9000}
	calls := 0
	err := installLinuxSignalHandlers(&act, func(_ uintptr, _ *kernelSigaction, old *kernelSigaction) error {
		calls++
		if old != nil {
			old.handler = 0
		}
		return nil
	})
	if err == nil {
		t.Fatal("default SIGSEGV disposition was accepted")
	}
	if calls != 1 {
		t.Fatalf("rt_sigaction calls = %d, want 1", calls)
	}
}
