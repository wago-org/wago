(module
  (import "tutorial" "answer" (func $answer (result i32)))
  (func (export "run") (result i32)
    call $answer))
