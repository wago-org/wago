(module (memory 1 256) (func (export "f") (param $ptr i32) (param $n i32) (param $sum i32) (result i32 i32 i32)
(block $exit (loop $loop
(local.get $n) i32.eqz br_if $exit
(local.get $ptr) i32.load (local.set $ptr)
(local.get $sum) (local.get $ptr) i32.add (local.set $sum)
(local.get $n) i32.const 1 i32.sub (local.set $n) br $loop)) (local.get $ptr) (local.get $n) (local.get $sum)) )
