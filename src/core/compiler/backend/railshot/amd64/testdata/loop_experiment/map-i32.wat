(module (memory 1 1024) (func (export "f") (param $dst i32) (param $src i32) (param $n i32) (param $scale i32) (param $bias i32) (result i32 i32 i32 i32) (local $v i32)
(block $exit (loop $loop
(local.get $n) i32.eqz br_if $exit
(local.get $dst) (local.get $src) i32.load (local.get $scale) i32.mul (local.get $bias) i32.add (local.tee $v) i32.store
(local.get $dst) i32.const 4 i32.add (local.set $dst)
(local.get $src) i32.const 4 i32.add (local.set $src)
(local.get $n) i32.const 1 i32.sub (local.set $n) br $loop)) (local.get $dst) (local.get $src) (local.get $n) (local.get $v)) )
