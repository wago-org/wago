// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && arm64 && !tinygo && !wago_target_tinygo

package runtime

// Linux/arm64 supplies the signal-return trampoline; SA_RESTORER is an x86
// kernel ABI detail and must not be installed here.
func configureInterruptSigaction(*interruptSigaction) {}
