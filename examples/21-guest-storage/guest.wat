(module
  (import "storage" "fill" (func $fill (param i32 i32) (result i32)))
  (memory (export "memory") 1)

  (func (export "run") (result i32)
    i32.const 0
    i32.const 4
    call $fill
    drop
    i32.const 0
    i32.load8_u))
