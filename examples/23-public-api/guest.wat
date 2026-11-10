;; This Source Code Form is subject to the terms of the Mozilla Public
;; License, v. 2.0. If a copy of the MPL was not distributed with this
;; file, You can obtain one at https://mozilla.org/MPL/2.0/.
;;
;; SPDX-License-Identifier: MPL-2.0
;; Copyright (c) 2026 Wago authors and contributors

(module
  (import "env" "add" (func $add (param i32 i32) (result i32)))
  (import "foo" "scale" (func $scale (param i32 f64) (result f64)))
  (import "env" "log" (func $log (param i32 i32)))

  (memory (export "memory") 1)
  (data (i32.const 0) "run")

  (func (export "step") (param i32) (result i32)
    local.get 0
    i32.const 1
    call $add)

  (func (export "sum5") (param i32 i32 i32 i32 i32) (result i32)
    local.get 0
    local.get 1
    i32.add
    local.get 2
    i32.add
    local.get 3
    i32.add
    local.get 4
    i32.add)

  (func (export "run") (param i32 i32 f64) (result f64)
    local.get 0
    local.get 1
    call $log
    local.get 0
    local.get 1
    call $add
    local.get 2
    call $scale))
