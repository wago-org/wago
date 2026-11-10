#!/usr/bin/env sh
# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at https://mozilla.org/MPL/2.0/.
#
# SPDX-License-Identifier: MPL-2.0
# Copyright (c) 2026 Wago authors and contributors

set -eu

git rev-parse --git-dir >/dev/null 2>&1 || {
	printf '%s\n' "wago: not inside a git repository" >&2
	exit 1
}

git config core.hooksPath .githooks
printf '%s\n' "wago: installed git hooks from .githooks"
