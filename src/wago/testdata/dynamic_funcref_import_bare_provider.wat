;; This Source Code Form is subject to the terms of the Mozilla Public
;; License, v. 2.0. If a copy of the MPL was not distributed with this
;; file, You can obtain one at https://mozilla.org/MPL/2.0/.
;;
;; SPDX-License-Identifier: MPL-2.0
;; Copyright (c) 2026 Wago authors and contributors

(module
  (type $super (sub (func)))
  (type $sub (sub $super (func)))

  ;; Deliberately no ref.func, table, or element use: the provider must remain
  ;; descriptor-free so the consumer owns the imported proxy descriptor.
  (func (export "f") (type $sub))
)
