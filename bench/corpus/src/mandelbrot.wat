;; mandelbrot: escape-time render of the Mandelbrot set over an n x n grid,
;; summing the iteration counts (returned as i32 so the work isn't dead-code
;; eliminated). Pure f64 compute — no memory — so it exercises the floating
;; point codegen (mul/add/compare) and tight nested loops end to end, unlike the
;; short float.run micro.
(module
  (func (export "render") (param $n i32) (result i32)
    (local $px i32) (local $py i32) (local $iter i32) (local $total i32)
    (local $x0 f64) (local $y0 f64) (local $x f64) (local $y f64) (local $xt f64)
    (local $inv f64)
    (local.set $inv (f64.div (f64.const 1) (f64.convert_i32_s (local.get $n))))
    (block $rows
      (loop $row
        (br_if $rows (i32.ge_s (local.get $py) (local.get $n)))
        (local.set $px (i32.const 0))
        (block $cols
          (loop $col
            (br_if $cols (i32.ge_s (local.get $px) (local.get $n)))
            ;; map pixel -> complex plane: x0 in [-2.5, 1.0], y0 in [-1.0, 1.0]
            (local.set $x0 (f64.add (f64.mul (f64.mul (f64.convert_i32_s (local.get $px)) (local.get $inv)) (f64.const 3.5)) (f64.const -2.5)))
            (local.set $y0 (f64.add (f64.mul (f64.mul (f64.convert_i32_s (local.get $py)) (local.get $inv)) (f64.const 2.0)) (f64.const -1.0)))
            (local.set $x (f64.const 0))
            (local.set $y (f64.const 0))
            (local.set $iter (i32.const 0))
            (block $esc
              (loop $it
                (br_if $esc (f64.gt (f64.add (f64.mul (local.get $x) (local.get $x)) (f64.mul (local.get $y) (local.get $y))) (f64.const 4)))
                (br_if $esc (i32.ge_s (local.get $iter) (i32.const 100)))
                (local.set $xt (f64.add (f64.sub (f64.mul (local.get $x) (local.get $x)) (f64.mul (local.get $y) (local.get $y))) (local.get $x0)))
                (local.set $y (f64.add (f64.mul (f64.const 2) (f64.mul (local.get $x) (local.get $y))) (local.get $y0)))
                (local.set $x (local.get $xt))
                (local.set $iter (i32.add (local.get $iter) (i32.const 1)))
                (br $it)))
            (local.set $total (i32.add (local.get $total) (local.get $iter)))
            (local.set $px (i32.add (local.get $px) (i32.const 1)))
            (br $col)))
        (local.set $py (i32.add (local.get $py) (i32.const 1)))
        (br $row)))
    (local.get $total)))
