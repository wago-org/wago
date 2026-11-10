;; This Source Code Form is subject to the terms of the Mozilla Public
;; License, v. 2.0. If a copy of the MPL was not distributed with this
;; file, You can obtain one at https://mozilla.org/MPL/2.0/.
;;
;; SPDX-License-Identifier: MPL-2.0
;; Copyright (c) 2026 Wago authors and contributors

;; memory: linear-memory load/store loop (8-byte stride) — load/store path.
(module
  (memory 1)
  (func (export "fill") (param i32)
    (local i32) ;; 1:ptr
    (block $brk
      (loop $lp
        (br_if $brk (i32.eqz (local.get 0)))
        (i64.store (local.get 1) (i64.extend_i32_u (local.get 0)))
        (local.set 1 (i32.add (local.get 1) (i32.const 8)))
        (local.set 0 (i32.sub (local.get 0) (i32.const 1)))
        (br $lp))))
  (func (export "sum") (param i32) (result i64)
    (local i32 i64) ;; 1:ptr 2:acc
    (local.set 2 (i64.const 0))
    (block $brk
      (loop $lp
        (br_if $brk (i32.eqz (local.get 0)))
        (local.set 2 (i64.add (local.get 2) (i64.load (local.get 1))))
        (local.set 1 (i32.add (local.get 1) (i32.const 8)))
        (local.set 0 (i32.sub (local.get 0) (i32.const 1)))
        (br $lp)))
    (local.get 2)))
