(module (memory 1 256) (func (export "f") (param $dst i32) (param $src i32) (param $n i32) (param $delta v128) (result i32 i32 i32 v128) (local $v v128)
(block $exit (loop $loop
(local.get $n) i32.eqz br_if $exit
(local.get $dst) (local.get $src) v128.load (local.get $delta) i32x4.add (local.tee $v) v128.store
(local.get $dst) i32.const 16 i32.add (local.set $dst)
(local.get $src) i32.const 16 i32.add (local.set $src)
(local.get $n) i32.const 1 i32.sub (local.set $n) br $loop)) (local.get $dst) (local.get $src) (local.get $n) (local.get $v)) )
