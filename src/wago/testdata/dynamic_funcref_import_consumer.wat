;; This Source Code Form is subject to the terms of the Mozilla Public
;; License, v. 2.0. If a copy of the MPL was not distributed with this
;; file, You can obtain one at https://mozilla.org/MPL/2.0/.
;;
;; SPDX-License-Identifier: MPL-2.0
;; Copyright (c) 2026 Wago authors and contributors

(module
  (type $super (sub (func)))
  (type $sub (sub $super (func)))

  (import "env" "f" (func $import (type $super)))
  (table 1 funcref)
  (elem (i32.const 0) func $import)

  (func (export "test_super") (result i32)
    (ref.test (ref $super) (table.get 0 (i32.const 0))))

  (func (export "test_sub") (result i32)
    (ref.test (ref $sub) (table.get 0 (i32.const 0))))
)
