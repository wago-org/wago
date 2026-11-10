(module
  (import "example:types" "zero"
    (func $zero (result externref)))
  (import "example:types" "discard"
    (func $discard (param externref)))

  (func (export "run")
    call $zero
    call $discard))
