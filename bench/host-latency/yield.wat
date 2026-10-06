(module
  (import "env" "step" (func $step (param i32) (result i32)))
  (memory 1)
  (func (export "run") (param $count i32) (param $unused i32) (result i32)
    (local $sum i32)
    (block $done
      (loop $next
        (br_if $done (i32.eqz (local.get $count)))
        (local.set $sum (call $step (local.get $sum)))
        (local.set $count (i32.sub (local.get $count) (i32.const 1)))
        (br $next)))
    (local.get $sum)))
