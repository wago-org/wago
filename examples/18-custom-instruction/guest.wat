;; This Source Code Form is subject to the terms of the Mozilla Public
;; License, v. 2.0. If a copy of the MPL was not distributed with this
;; file, You can obtain one at https://mozilla.org/MPL/2.0/.
;;
;; SPDX-License-Identifier: MPL-2.0
;; Copyright (c) 2026 Wago authors and contributors

(module
  (import "wago:instr/example.int" "i4.add"
    (func $i4.add (param i32 i32) (result i32)))

  (func (export "add") (param i32 i32) (result i32)
    local.get 0
    local.get 1
    call $i4.add))
