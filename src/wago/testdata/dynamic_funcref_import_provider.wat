;; This Source Code Form is subject to the terms of the Mozilla Public
;; License, v. 2.0. If a copy of the MPL was not distributed with this
;; file, You can obtain one at https://mozilla.org/MPL/2.0/.
;;
;; SPDX-License-Identifier: MPL-2.0
;; Copyright (c) 2026 Wago authors and contributors

(module
  (type $super (sub (func)))
  (type $sub (sub $super (func)))

  (func $f (export "f") (type $sub))
  (func (export "get") (result (ref $sub))
    (ref.func $f))
  (elem declare func $f)
)
