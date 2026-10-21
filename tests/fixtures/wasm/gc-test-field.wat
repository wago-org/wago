(module
(type $box (struct (field i32) (field i64) (field eqref)))
(type $child (struct (field i32)))
(type $other (struct (field i64)))
(type $open (sub (struct (field i32))))
(type $empty (struct))
(type $packed (struct (field i8) (field i16)))
(global $root (mut eqref) (ref.null eq))
(global $allocation (mut eqref) (ref.null eq))
(global $g0 (mut i64) (i64.const 0))
(global $g1 (mut i64) (i64.const 0))
(global $g2 (mut i64) (i64.const 0))
(global $g3 (mut i64) (i64.const 0))
(global $g4 (mut i64) (i64.const 0))
(global $g5 (mut i64) (i64.const 0))
(func (export "init") i32.const 7 i64.const 9 i32.const 17 struct.new $child struct.new $box global.set $root)
(func (export "clear") ref.null eq global.set $root)
(func (export "wrong") i64.const 3 struct.new $other global.set $root)
(func (export "i31") i32.const 8 ref.i31 global.set $root)
(func $read (export "read") (param $obj eqref) (result i32)
 local.get $obj ref.test (ref $box)
 if (result i32) local.get $obj ref.cast (ref $box) struct.get $box 0 else i32.const 99 end)
(func $read-nop (param $obj eqref) (result i32)
 local.get $obj ref.test (ref $box)
 if (result i32) nop local.get $obj ref.cast (ref $box) struct.get $box 0 else i32.const 99 end)
(func (export "read-global") (result i32) global.get $root call $read)
(func (export "i64") (result i64) (local $obj eqref)
 global.get $root local.set $obj local.get $obj ref.test (ref $box)
 if (result i64) local.get $obj ref.cast (ref null $box) struct.get $box 1 else i64.const 99 end)
(func (export "root") (result i32) (local $obj eqref) (local $saved eqref)
 global.get $root local.set $obj local.get $obj ref.test (ref $box)
 if (result eqref) local.get $obj ref.cast (ref $box) struct.get $box 2 else ref.null eq end
 local.set $saved ref.null eq global.set $root struct.new $empty global.set $allocation
 local.get $saved ref.cast (ref $child) struct.get $child 0)
(func (export "nop") (result i32) (local $obj eqref)
 global.get $root local.set $obj local.get $obj ref.test (ref $box)
 if (result i32) nop local.get $obj ref.cast (ref $box) struct.get $box 0 else i32.const 99 end)
(func (export "nullable") (result i32) (local $obj eqref)
 global.get $root local.set $obj local.get $obj ref.test (ref null $box)
 if (result i32) local.get $obj ref.cast (ref $box) struct.get $box 0 else i32.const 99 end)
(func (export "different-local") (result i32) (local $a eqref) (local $b eqref)
 global.get $root local.tee $a local.set $b local.get $a ref.test (ref $box)
 if (result i32) local.get $b ref.cast (ref $box) struct.get $box 0 else i32.const 99 end)
(func (export "nonfinal") (result i32) (local $obj eqref)
 i32.const 5 struct.new $open local.set $obj local.get $obj ref.test (ref $open)
 if (result i32) local.get $obj ref.cast (ref $open) struct.get $open 0 else i32.const 99 end)
(func (export "packed-s") (result i32) (local $obj eqref)
 i32.const -2 i32.const -3 struct.new $packed local.set $obj local.get $obj ref.test (ref $packed)
 if (result i32) local.get $obj ref.cast (ref $packed) struct.get_s $packed 0 else i32.const 99 end)
(func (export "packed-u") (result i32) (local $obj eqref)
 i32.const -2 i32.const -3 struct.new $packed local.set $obj local.get $obj ref.test (ref $packed)
 if (result i32) local.get $obj ref.cast (ref $packed) struct.get_u $packed 1 else i32.const 99 end)
(func $opaque (param i64) (result i64) global.get $allocation drop local.get 0)
(func (export "pressure") (param i64 i64 i64 i64 i64 i64 i64 i64) (result i64) (local $obj eqref)
 global.get $root local.set $obj local.get $obj ref.test (ref $box)
 if (result i32) local.get $obj ref.cast (ref $box) struct.get $box 0 else i32.const 99 end drop
 local.get 0 call $opaque local.get 1 i64.add local.get 2 i64.add local.get 3 i64.add local.get 4 i64.add local.get 5 i64.add local.get 6 i64.add local.get 7 i64.add)
(func (export "loop") (param $n i32) (result i32) (local $sum i32)
 (block $done local.get $n i32.eqz br_if $done (loop $again
 local.get $sum global.get $root call $read i32.add local.set $sum
 local.get $n i32.const 1 i32.sub local.tee $n br_if $again)) local.get $sum)
(func (export "loop-nop") (param $n i32) (result i32) (local $sum i32)
 (block $done local.get $n i32.eqz br_if $done (loop $again
 local.get $sum global.get $root call $read-nop i32.add local.set $sum
 local.get $n i32.const 1 i32.sub local.tee $n br_if $again)) local.get $sum)
(func (export "dirty") (result i32) (local $obj eqref)
 global.get $root local.set $obj
 loop
 local.get $obj drop local.get $obj drop local.get $obj drop
 global.get $g0 i64.const 1 i64.add global.set $g0
 global.get $g1 i64.const 2 i64.add global.set $g1
 global.get $g2 i64.const 3 i64.add global.set $g2
 global.get $g3 i64.const 4 i64.add global.set $g3
 global.get $g4 i64.const 5 i64.add global.set $g4
 global.get $g5 i64.const 6 i64.add global.set $g5
 end
 local.get $obj ref.test (ref $box)
 if (result i32) local.get $obj ref.cast (ref $box) struct.get $box 0 else i32.const 99 end)
(func (export "stack-root") (result i32) (local $obj eqref)
 global.get $root local.set $obj
 local.get $obj ref.cast (ref $box) struct.get $box 2
 local.get $obj ref.test (ref $box)
 if (result i32) local.get $obj ref.cast (ref $box) struct.get $box 0 else i32.const 99 end drop
 ref.null eq global.set $root struct.new $empty global.set $allocation
 ref.cast (ref $child) struct.get $child 0)
(func (export "budget") (param $obj eqref) (result i32)
 local.get $obj ref.test (ref $box) if (result i32) local.get $obj ref.cast (ref $box) struct.get $box 0 else i32.const 99 end drop
 local.get $obj ref.test (ref $box) if (result i32) local.get $obj ref.cast (ref $box) struct.get $box 0 else i32.const 99 end drop
 local.get $obj ref.test (ref $box) if (result i32) local.get $obj ref.cast (ref $box) struct.get $box 0 else i32.const 99 end drop
 local.get $obj ref.test (ref $box) if (result i32) local.get $obj ref.cast (ref $box) struct.get $box 0 else i32.const 99 end drop
 local.get $obj ref.test (ref $box) if (result i32) local.get $obj ref.cast (ref $box) struct.get $box 0 else i32.const 99 end drop
 local.get $obj ref.test (ref $box) if (result i32) local.get $obj ref.cast (ref $box) struct.get $box 0 else i32.const 99 end drop
 local.get $obj ref.test (ref $box) if (result i32) local.get $obj ref.cast (ref $box) struct.get $box 0 else i32.const 99 end drop
 local.get $obj ref.test (ref $box) if (result i32) local.get $obj ref.cast (ref $box) struct.get $box 0 else i32.const 99 end drop
 local.get $obj ref.test (ref $box) if (result i32) local.get $obj ref.cast (ref $box) struct.get $box 0 else i32.const 99 end drop
 local.get $obj ref.test (ref $box) if (result i32) local.get $obj ref.cast (ref $box) struct.get $box 0 else i32.const 99 end drop
 local.get $obj ref.test (ref $box) if (result i32) local.get $obj ref.cast (ref $box) struct.get $box 0 else i32.const 99 end drop
 local.get $obj ref.test (ref $box) if (result i32) local.get $obj ref.cast (ref $box) struct.get $box 0 else i32.const 99 end drop
 local.get $obj ref.test (ref $box) if (result i32) local.get $obj ref.cast (ref $box) struct.get $box 0 else i32.const 99 end drop
 local.get $obj ref.test (ref $box) if (result i32) local.get $obj ref.cast (ref $box) struct.get $box 0 else i32.const 99 end drop
 local.get $obj ref.test (ref $box) if (result i32) local.get $obj ref.cast (ref $box) struct.get $box 0 else i32.const 99 end drop
 local.get $obj ref.test (ref $box) if (result i32) local.get $obj ref.cast (ref $box) struct.get $box 0 else i32.const 99 end drop
 local.get $obj ref.test (ref $box) if (result i32) local.get $obj ref.cast (ref $box) struct.get $box 0 else i32.const 99 end drop
 i32.const 7)
)
