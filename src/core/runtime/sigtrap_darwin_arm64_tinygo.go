// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build darwin && arm64 && tinygo && wago_guardpage

package runtime

/*
#include <errno.h>
#include <signal.h>
#include <stddef.h>
#include <stdint.h>
#include <sys/ucontext.h>

typedef struct {
	uintptr_t start, end, memory, owner;
} wago_guard_region;

static wago_guard_region *wago_regions;
static struct sigaction wago_guard_action, wago_previous_segv, wago_previous_bus;

// Restore the native entry frame recorded by enterNative and jump directly to
// its continuation. x9 carries the owning memory base from ucontext.
__attribute__((naked)) static void wago_guard_exit(void) {
	__asm__ volatile (
		"ldur x10, [x9, #-24]\n"
		"mov sp, x10\n"
		"ldur x10, [x9, #-32]\n"
		"br x10\n");
}

// Commit one 64-KiB guest page without entering TinyGo or libc on the
// interrupted native stack. Darwin reports failed syscalls in the carry bit.
static int wago_commit_page(uintptr_t address) {
	register uintptr_t x0 __asm__("x0") = address;
	register uintptr_t x1 __asm__("x1") = 65536;
	register uintptr_t x2 __asm__("x2") = 3;
	register uintptr_t x16 __asm__("x16") = 74;
	__asm__ volatile ("svc #0x80\n\tcset %w0, cs"
		: "+r"(x0) : "r"(x1), "r"(x2), "r"(x16) : "cc", "memory");
	return (int)x0;
}

static void wago_guard_signal(int sig, siginfo_t *info, void *opaque) {
	ucontext_t *context = opaque;
	_STRUCT_ARM_THREAD_STATE64 *state = &context->uc_mcontext->__ss;
	uintptr_t address = (uintptr_t)info->si_addr;
	uintptr_t owner = state->__x[26];
	for (int i = 0; i < 256; i++) {
		const wago_guard_region *region = &wago_regions[i];
		uintptr_t start = __atomic_load_n(&region->start, __ATOMIC_ACQUIRE);
		if (!start || address < start || address >= region->end || owner != region->owner) {
			continue;
		}
		uintptr_t offset = address - region->memory;
		uint32_t trap = 3;
		if (offset < *(uint64_t *)(region->memory - 288)) {
			if (!wago_commit_page(region->memory + (offset & ~(uintptr_t)65535))) {
				return;
			}
			trap = 4;
		}

		uint32_t *cell = *(uint32_t **)(owner - 104);
		if (cell == NULL) {
			break;
		}
		*cell = trap;
		state->__x[9] = owner;
		__darwin_arm_thread_state64_set_pc_fptr(*state, &wago_guard_exit);
		return;
	}
	const struct sigaction *previous = sig == SIGBUS ? &wago_previous_bus : &wago_previous_segv;
	if (previous->sa_flags & SA_SIGINFO) {
		previous->sa_sigaction(sig, info, opaque);
	} else {
		previous->sa_handler(sig);
	}
	// TinyGo's SA_RESETHAND handler re-registers itself after arranging a
	// recoverable Go panic. Rearm the Wasm handler when it did so.
	struct sigaction current;
	if (sigaction(sig, NULL, &current) == 0) {
		int same_handler = previous->sa_flags & SA_SIGINFO
		    ? (current.sa_flags & SA_SIGINFO) && current.sa_sigaction == previous->sa_sigaction
		    : !(current.sa_flags & SA_SIGINFO) && current.sa_handler == previous->sa_handler;
		if (same_handler) {
			sigaction(sig, &wago_guard_action, NULL);
		}
	}
}

static int wago_install_guard(void *regions) {
	wago_guard_action.sa_sigaction = wago_guard_signal;
	wago_guard_action.sa_flags = SA_SIGINFO | SA_ONSTACK | SA_RESTART;
	sigfillset(&wago_guard_action.sa_mask);
	if (sigaction(SIGSEGV, NULL, &wago_previous_segv) ||
	    sigaction(SIGBUS, NULL, &wago_previous_bus)) {
		return errno;
	}
	if ((uintptr_t)wago_previous_segv.sa_handler <= 1 ||
	    (uintptr_t)wago_previous_bus.sa_handler <= 1) {
		return EINVAL;
	}
	wago_regions = regions;
	if (sigaction(SIGSEGV, &wago_guard_action, NULL)) {
		return errno;
	}
	if (sigaction(SIGBUS, &wago_guard_action, NULL)) {
		int saved_errno = errno;
		if (sigaction(SIGSEGV, &wago_previous_segv, NULL)) {
			return errno;
		}
		return saved_errno;
	}
	return 0;
}
*/
import "C"

import (
	"fmt"
	"syscall"
	"unsafe"

	"github.com/wago-org/wago/src/core/runtime/abi"
)

// The TinyGo signal and landing-pad code use the same basedata ABI as the
// ordinary Go Darwin arm64 signal entry.
const (
	_ = uint(abi.ActualLinMemByteSize64Offset - 288)
	_ = uint(288 - abi.ActualLinMemByteSize64Offset)
	_ = uint(abi.TrapCellPtrOffset - 104)
	_ = uint(104 - abi.TrapCellPtrOffset)
	_ = uint(offTrapStackReentry - 24)
	_ = uint(24 - offTrapStackReentry)
	_ = uint(arm64TrapHandlerPtrOffset - 32)
	_ = uint(32 - arm64TrapHandlerPtrOffset)
	_ = uint(TrapLinMemOutOfBounds - 3)
	_ = uint(3 - TrapLinMemOutOfBounds)
	_ = uint(TrapLinMemCouldNotExtend - 4)
	_ = uint(4 - TrapLinMemCouldNotExtend)
	_ = uint(maxGuardRegions - 256)
	_ = uint(256 - maxGuardRegions)
)

func installDarwinSignalHandlers() error {
	if errno := C.wago_install_guard(unsafe.Pointer(&guardRegions[0])); errno != 0 {
		return fmt.Errorf("install TinyGo Darwin guard handler: %w", syscall.Errno(errno))
	}
	return nil
}
