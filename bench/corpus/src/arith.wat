;; arith: mixed i32/i64 arithmetic (mul/xor/shift) in a loop — ALU bound.
(module
  (func (export "run") (param i32) (result i64)
    (local i64) ;; 1:acc
    (local.set 1 (i64.const 0))
    (block $brk
      (loop $lp
        (br_if $brk (i32.eqz (local.get 0)))
        (local.set 1
          (i64.add (local.get 1)
            (i64.mul (i64.extend_i32_s (local.get 0)) (i64.const 2654435761))))
        (local.set 1 (i64.xor (local.get 1) (i64.shr_u (local.get 1) (i64.const 13))))
        (local.set 0 (i32.sub (local.get 0) (i32.const 1)))
        (br $lp)))
    (local.get 1)))
