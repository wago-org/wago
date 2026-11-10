(module
  ;; A returning host import: env.host(i32) -> i32. The guest calls it once and
  ;; returns its result — one full wasm -> host -> wasm roundtrip per invocation.
  (import "env" "host" (func $host (param i32) (result i32)))
  (func (export "roundtrip") (param i32) (result i32)
    local.get 0
    call $host))
