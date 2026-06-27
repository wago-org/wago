;; fib_iter: tight integer loop (branch + local traffic, no calls).
(module
  (func (export "fib") (param i32) (result i64)
    (local i64 i64 i64) ;; 1:a 2:b 3:t
    (local.set 1 (i64.const 0))
    (local.set 2 (i64.const 1))
    (block $brk
      (loop $lp
        (br_if $brk (i32.eqz (local.get 0)))
        (local.set 3 (i64.add (local.get 1) (local.get 2)))
        (local.set 1 (local.get 2))
        (local.set 2 (local.get 3))
        (local.set 0 (i32.sub (local.get 0) (i32.const 1)))
        (br $lp)))
    (local.get 1)))
