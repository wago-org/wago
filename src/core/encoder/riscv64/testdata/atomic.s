# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at https://mozilla.org/MPL/2.0/.
#
# SPDX-License-Identifier: MPL-2.0
# Copyright (c) 2026 Wago authors and contributors

.text
.option norvc
lr.w x5, (x6)
lr.w.aq x5, (x6)
lr.w.rl x5, (x6)
lr.w.aqrl x5, (x6)
sc.w x5, x7, (x6)
sc.w.aq x5, x7, (x6)
sc.w.rl x5, x7, (x6)
sc.w.aqrl x5, x7, (x6)
lr.d x5, (x6)
lr.d.aq x5, (x6)
lr.d.rl x5, (x6)
lr.d.aqrl x5, (x6)
sc.d x5, x7, (x6)
sc.d.aq x5, x7, (x6)
sc.d.rl x5, x7, (x6)
sc.d.aqrl x5, x7, (x6)
