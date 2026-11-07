(module
 (import "env" "step" (func $step (param i64) (result i64)))
 (func (export "run") (param i64) (result i64)
  local.get 0 call $step))
