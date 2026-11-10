;; This Source Code Form is subject to the terms of the Mozilla Public
;; License, v. 2.0. If a copy of the MPL was not distributed with this
;; file, You can obtain one at https://mozilla.org/MPL/2.0/.
;;
;; SPDX-License-Identifier: MPL-2.0
;; Copyright (c) 2026 Wago authors and contributors

(module
  ;; A returning host import: env.host(i32) -> i32. The guest calls it once and
  ;; returns its result — one full wasm -> host -> wasm roundtrip per invocation.
  (import "env" "host" (func $host (param i32) (result i32)))
  (func (export "roundtrip") (param i32) (result i32)
    local.get 0
    call $host))
