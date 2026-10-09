(module (memory 1 256) (func (export "f") (param $dst i32) (param $src i32) (param $n i32) (param $sum f64) (result i32 i32 i32 f64)
(block $exit (loop $loop
(local.get $n) i32.eqz br_if $exit
(local.get $dst) (local.get $sum) (local.get $src) f64.load f64.add (local.tee $sum) f64.store
(local.get $dst) i32.const 8 i32.add (local.set $dst)
(local.get $src) i32.const 8 i32.add (local.set $src)
(local.get $n) i32.const 1 i32.sub (local.set $n) br $loop)) (local.get $dst) (local.get $src) (local.get $n) (local.get $sum)) )
