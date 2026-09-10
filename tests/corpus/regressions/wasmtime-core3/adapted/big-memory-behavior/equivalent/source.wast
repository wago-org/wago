;; Bounded state-machine projection of upstream big-memory-behavior.wast.
(module
  (memory 1 2)
  (func (export "grow") (param i32) (result i32)
    local.get 0
    memory.grow)
  (func (export "size") (result i32)
    memory.size)
)
(assert_return (invoke "grow" (i32.const 0)) (i32.const 1))
(assert_return (invoke "size") (i32.const 1))
(assert_return (invoke "grow" (i32.const 1)) (i32.const 1))
(assert_return (invoke "size") (i32.const 2))
(assert_return (invoke "grow" (i32.const 0)) (i32.const 2))
(assert_return (invoke "grow" (i32.const 1)) (i32.const -1))
