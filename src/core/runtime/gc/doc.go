// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

// Package gc provides collector-bound, generation-checked Go references.
// Object references are opaque values and cannot be transferred between
// collectors. Keep live objects in RootSets across collection. Collectors and
// their roots require external synchronization, as does the native collector.
//
// The native subpackage is the trusted JIT ABI. Its compact integers deliberately
// carry no ownership information and must never serve as a Go ingress API.
package gc
