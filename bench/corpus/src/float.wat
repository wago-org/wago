;; float: f64 math (mul/add/sqrt/convert) in a loop — FP pipeline.
(module
  (func (export "run") (param i32) (result f64)
    (local f64) ;; 1:acc
    (local.set 1 (f64.const 1))
    (block $brk
      (loop $lp
        (br_if $brk (i32.eqz (local.get 0)))
        (local.set 1
          (f64.add
            (f64.mul (local.get 1) (f64.const 1.0000001))
            (f64.sqrt (f64.convert_i32_u (local.get 0)))))
        (local.set 0 (i32.sub (local.get 0) (i32.const 1)))
        (br $lp)))
    (local.get 1)))
