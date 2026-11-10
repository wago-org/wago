// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

/* Brotli's optional file-metadata copy calls chown(), which WASI lacks.
 * The streaming corpus invocation never calls that path. */
#define chown(path, owner, group) (0)
