;; This Source Code Form is subject to the terms of the Mozilla Public
;; License, v. 2.0. If a copy of the MPL was not distributed with this
;; file, You can obtain one at https://mozilla.org/MPL/2.0/.
;;
;; SPDX-License-Identifier: MPL-2.0
;; Copyright (c) 2026 Wago authors and contributors

;; tiny: minimal module — exercises fixed per-module pipeline overhead.
(module
  (func (export "answer") (result i32)
    i32.const 42)
  (func (export "add") (param i32 i32) (result i32)
    local.get 0
    local.get 1
    i32.add))
