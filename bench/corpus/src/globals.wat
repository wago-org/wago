;; globals: mutable global get/set in a loop.
(module
  (global $g (mut i64) (i64.const 0))
  (func (export "accumulate") (param i32) (result i64)
    (block $brk
      (loop $lp
        (br_if $brk (i32.eqz (local.get 0)))
        (global.set $g (i64.add (global.get $g) (i64.extend_i32_u (local.get 0))))
        (local.set 0 (i32.sub (local.get 0) (i32.const 1)))
        (br $lp)))
    (global.get $g))
  (func (export "get") (result i64) (global.get $g)))
