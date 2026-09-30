(module
  (import "env" "outer" (func $outer (param i32) (result i32)))

  (func (export "run") (param i32) (result i32)
    local.get 0
    call $outer)

  (func (export "callback") (param i32) (result i32)
    local.get 0
    i32.const 1
    i32.add))
