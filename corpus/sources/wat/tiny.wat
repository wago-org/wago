;; tiny: minimal module — exercises fixed per-module pipeline overhead.
(module
  (func (export "answer") (result i32)
    i32.const 42)
  (func (export "add") (param i32 i32) (result i32)
    local.get 0
    local.get 1
    i32.add))
