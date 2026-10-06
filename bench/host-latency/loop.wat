(module
  (import "env" "step" (func $step (param i32) (result i32)))
  (memory 1)
  (func (export "run") (param $count i32) (param $host i32) (result i32)
    (local $sum i32)
    (block $done
      (loop $next
        (br_if $done (i32.eqz (local.get $count)))
        (local.set $sum
          (if (result i32) (local.get $host)
            (then (call $step (local.get $sum)))
            (else (i32.add (local.get $sum) (i32.const 1)))))
        (local.set $count (i32.sub (local.get $count) (i32.const 1)))
        (br $next)))
    (local.get $sum)))
