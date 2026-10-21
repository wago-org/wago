(module
 (import "env" "step" (func $step (param i64) (result i64)))
 (func (export "run") (param i64) (result i64)
  (loop $repeat
   local.get 0 call $step drop
   br $repeat)
  i64.const 0))
