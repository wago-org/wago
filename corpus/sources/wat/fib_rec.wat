;; This Source Code Form is subject to the terms of the Mozilla Public
;; License, v. 2.0. If a copy of the MPL was not distributed with this
;; file, You can obtain one at https://mozilla.org/MPL/2.0/.
;;
;; SPDX-License-Identifier: MPL-2.0
;; Copyright (c) 2026 Wago authors and contributors

;; fib_rec: recursion — call/return boundary heavy, if/else with block result.
(module
  (func $fib (export "fib") (param i32) (result i64)
    (if (result i64) (i32.lt_s (local.get 0) (i32.const 2))
      (then (i64.extend_i32_s (local.get 0)))
      (else
        (i64.add
          (call $fib (i32.sub (local.get 0) (i32.const 1)))
          (call $fib (i32.sub (local.get 0) (i32.const 2))))))))
