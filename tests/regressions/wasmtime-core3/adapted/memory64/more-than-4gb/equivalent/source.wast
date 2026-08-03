;; Finite-reservation projection of upstream memory64/more-than-4gb.wast.
;; The 0x1_0000_0000 base is translated to the last supported 64-KiB page.
(module $memory
  (memory (export "memory") i64 0xffff 0xffff)
)

(module
  (import "memory" "memory" (memory i64 0))
  (func (export "grow") (param i64) (result i64)
    local.get 0
    memory.grow)
  (func (export "size") (result i64)
    memory.size)
)
(assert_return (invoke "grow" (i64.const 0)) (i64.const 0xffff))
(assert_return (invoke "size") (i64.const 0xffff))
(assert_return (invoke "grow" (i64.const 1)) (i64.const -1))
(assert_return (invoke "size") (i64.const 0xffff))

(module $offset
  (global (export "offset") i64 (i64.const 0xfffe_0000))
)
(module
  (import "offset" "offset" (global i64))
  (import "memory" "memory" (memory i64 0))
  (data (global.get 0) "\01\02\03\04")
  (func (export "load32") (param i64) (result i32)
    local.get 0
    i32.load)
)
(assert_return (invoke "load32" (i64.const 0xfffe_0000)) (i32.const 0x04030201))

(module $offset
  (global (export "offset") i64 (i64.const 0xfffe_0000))
)
(module
  (import "memory" "memory" (memory i64 0))
  (data (i64.const 0xfffe_0004) "\01\02\03\04")
  (func (export "load32") (param i64) (result i32)
    local.get 0
    i32.load)
)
(assert_return (invoke "load32" (i64.const 0xfffe_0004)) (i32.const 0x04030201))

(module $offset
  (global (export "offset") i64 (i64.const 0xfffe_0000))
)
(module
  (import "memory" "memory" (memory i64 0))
  (data (i64.const 0xfffe_0004) "\01\02\03\04")
  (func (export "load32") (param i64) (result i32)
    local.get 0
    i32.load offset=0xfffe0000)
)
(assert_return (invoke "load32" (i64.const 2)) (i32.const 0x02010403))
