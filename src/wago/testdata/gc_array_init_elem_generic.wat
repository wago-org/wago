;; This Source Code Form is subject to the terms of the Mozilla Public
;; License, v. 2.0. If a copy of the MPL was not distributed with this
;; file, You can obtain one at https://mozilla.org/MPL/2.0/.
;;
;; SPDX-License-Identifier: MPL-2.0
;; Copyright (c) 2026 Wago authors and contributors

(module
  ;; Keep the exercised array and element segment away from index zero so the
  ;; fixture detects any reintroduction of the old hard-coded helper boundary.
  (type $dummy (array (mut i32)))
  (type $array (array (mut funcref)))
  (type $func (func (result i32)))

  (func $one (type $func) (result i32)
    i32.const 1)

  (elem $dummy-values funcref)
  (elem $values funcref
    (ref.func $one)
    (ref.null func))

  (func (export "run") (result i32)
    (local $array (ref $array))

    i32.const 2
    array.new_default $array
    local.set $array

    local.get $array
    i32.const 0
    i32.const 0
    i32.const 1
    array.init_elem $array $values

    local.get $array
    i32.const 0
    array.get $array
    ref.is_null
    i32.eqz)
)
