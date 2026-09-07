(module
  (import "wago:instr/example.int" "i4.add"
    (func $i4.add (param i32 i32) (result i32)))

  (func (export "add") (param i32 i32) (result i32)
    local.get 0
    local.get 1
    call $i4.add))
