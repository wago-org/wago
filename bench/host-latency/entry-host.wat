(module
  (import "env" "step" (func $step (param i32) (result i32)))
  (memory 1)
  (func (export "add") (param i32) (result i32)
    local.get 0
    call $step))
