// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wago_guardpage && darwin && (amd64 || arm64)

package runtime

import "syscall"

func growGuardedHostView(j *JobMemory, logicalBytes int) error {
	committed := len(j.mem) - j.linOff
	if logicalBytes <= committed {
		return nil
	}
	end := j.linOff + logicalBytes
	full := j.mem[:end]
	if err := syscall.Mprotect(full[j.linOff+committed:end], syscall.PROT_READ|syscall.PROT_WRITE); err != nil {
		return err
	}
	j.mem = full
	return nil
}
