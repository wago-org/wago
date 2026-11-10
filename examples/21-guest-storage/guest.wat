;; This Source Code Form is subject to the terms of the Mozilla Public
;; License, v. 2.0. If a copy of the MPL was not distributed with this
;; file, You can obtain one at https://mozilla.org/MPL/2.0/.
;;
;; SPDX-License-Identifier: MPL-2.0
;; Copyright (c) 2026 Wago authors and contributors

(module
  (import "storage" "fill" (func $fill (param i32 i32) (result i32)))
  (memory (export "memory") 1)

  (func (export "run") (result i32)
    i32.const 0
    i32.const 4
    call $fill
    drop
    i32.const 0
    i32.load8_u))
