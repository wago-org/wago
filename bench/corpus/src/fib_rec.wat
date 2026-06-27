;; fib_rec: recursion — call/return boundary heavy, if/else with block result.
(module
  (func $fib (export "fib") (param i32) (result i64)
    (if (result i64) (i32.lt_s (local.get 0) (i32.const 2))
      (then (i64.extend_i32_s (local.get 0)))
      (else
        (i64.add
          (call $fib (i32.sub (local.get 0) (i32.const 1)))
          (call $fib (i32.sub (local.get 0) (i32.const 2))))))))
