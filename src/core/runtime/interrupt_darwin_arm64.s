// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build darwin && arm64 && !tinygo && !wago_target_tinygo

#include "textflag.h"

TEXT ·addrMachTaskSelf(SB), NOSPLIT, $0-8
	MOVD $libc_mach_task_self(SB), R0
	MOVD R0, ret+0(FP)
	RET
TEXT ·addrMachThreadSelf(SB), NOSPLIT, $0-8
	MOVD $libc_mach_thread_self(SB), R0
	MOVD R0, ret+0(FP)
	RET
TEXT ·addrMachTaskThreads(SB), NOSPLIT, $0-8
	MOVD $libc_task_threads(SB), R0
	MOVD R0, ret+0(FP)
	RET
TEXT ·addrMachThreadSuspend(SB), NOSPLIT, $0-8
	MOVD $libc_thread_suspend(SB), R0
	MOVD R0, ret+0(FP)
	RET
TEXT ·addrMachThreadResume(SB), NOSPLIT, $0-8
	MOVD $libc_thread_resume(SB), R0
	MOVD R0, ret+0(FP)
	RET
TEXT ·addrMachThreadGetState(SB), NOSPLIT, $0-8
	MOVD $libc_thread_get_state(SB), R0
	MOVD R0, ret+0(FP)
	RET
TEXT ·addrMachThreadSetState(SB), NOSPLIT, $0-8
	MOVD $libc_thread_set_state(SB), R0
	MOVD R0, ret+0(FP)
	RET
TEXT ·addrMachPortDeallocate(SB), NOSPLIT, $0-8
	MOVD $libc_mach_port_deallocate(SB), R0
	MOVD R0, ret+0(FP)
	RET
TEXT ·addrMachVMDeallocate(SB), NOSPLIT, $0-8
	MOVD $libc_mach_vm_deallocate(SB), R0
	MOVD R0, ret+0(FP)
	RET

TEXT ·addrMachVMReadOverwrite(SB), NOSPLIT, $0-8
    MOVD $libc_mach_vm_read_overwrite(SB), R0
    MOVD R0, ret+0(FP)
    RET

// The rewritten state resumes with X9 naming active linear memory. The native
// trampoline published its foreign-stack save area and continuation in basedata.
TEXT ·darwinNativeInterruptTrap(SB), NOSPLIT|NOFRAME, $0-0
	MOVD -24(R9), R10
	MOVD R10, RSP
	MOVD -32(R9), R10
	B (R10)

TEXT ·addrDarwinNativeInterruptTrap(SB), NOSPLIT, $0-8
	MOVD $·darwinNativeInterruptTrap(SB), R0
	MOVD R0, ret+0(FP)
	RET
