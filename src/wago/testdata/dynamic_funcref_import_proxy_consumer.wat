;; This Source Code Form is subject to the terms of the Mozilla Public
;; License, v. 2.0. If a copy of the MPL was not distributed with this
;; file, You can obtain one at https://mozilla.org/MPL/2.0/.
;;
;; SPDX-License-Identifier: MPL-2.0
;; Copyright (c) 2026 Wago authors and contributors

(module
  (type $super (sub (func)))
  (type $sub (sub $super (func)))
  (type $box (struct (field (ref func))))

  (import "env" "f" (func $import (type $super)))
  (elem declare func $import)

  (func (export "test_direct") (result i32)
    (ref.test (ref $sub) (ref.func $import)))

  (func (export "test_stored") (result i32)
    (ref.test (ref $sub)
      (struct.get $box 0
        (struct.new $box (ref.func $import)))))
)
