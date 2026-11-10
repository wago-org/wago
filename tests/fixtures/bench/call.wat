;; This Source Code Form is subject to the terms of the Mozilla Public
;; License, v. 2.0. If a copy of the MPL was not distributed with this
;; file, You can obtain one at https://mozilla.org/MPL/2.0/.
;;
;; SPDX-License-Identifier: MPL-2.0
;; Copyright (c) 2026 Wago authors and contributors

(module
  ;; Boundary-only host -> Wasm fixture. Keeping the body to one identity
  ;; operation prevents guest computation from contaminating call overhead.
  (func (export "call") (param i32) (result i32)
    local.get 0))
