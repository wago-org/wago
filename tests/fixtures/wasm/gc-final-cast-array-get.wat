(module
(type $a (array (mut i32)))
(type $b (array (mut i8)))
(type $h (array (mut i16)))
(type $r (array (mut eqref)))
(type $f (array (mut f64)))
(type $v (array (mut v128)))
(type $s (struct))
(type $f32 (array (mut f32)))
(type $i64 (array (mut i64)))
(type $open (sub (array (mut i32))))
(global $array (mut eqref) (ref.null eq))
(global $refs (mut eqref) (ref.null eq))
(global $g0 (mut i64) (i64.const 0))
(global $g1 (mut i64) (i64.const 0))
(global $g2 (mut i64) (i64.const 0))
(global $g3 (mut i64) (i64.const 0))
(global $g4 (mut i64) (i64.const 0))
(global $g5 (mut i64) (i64.const 0))
(global $g6 (mut i64) (i64.const 0))
(global $g7 (mut i64) (i64.const 0))
(func (export "init") i32.const 7 i32.const 4 array.new $a global.set $array i32.const 13 ref.i31 i32.const 4 array.new $r global.set $refs)
(func (export "clear") ref.null eq global.set $array)
(func (export "wrong") struct.new $s global.set $array)
(func (export "read") (param i32) (result i32) global.get $array ref.cast (ref $a) local.get 0 array.get $a)
(func (export "nullable") (param i32) (result i32) global.get $array ref.cast (ref null $a) local.get 0 array.get $a)
(func (export "packed-s") (result i32) i32.const -2 i32.const 2 array.new $b ref.cast (ref $b) i32.const 1 array.get_s $b )
(func (export "packed-u") (result i32) i32.const -2 i32.const 2 array.new $b ref.cast (ref $b) i32.const 1 array.get_u $b )
(func (export "half-s") (result i32) i32.const -3 i32.const 2 array.new $h ref.cast (ref $h) i32.const 1 array.get_s $h )
(func (export "half-u") (result i32) i32.const -3 i32.const 2 array.new $h ref.cast (ref $h) i32.const 1 array.get_u $h )
(func (export "refs") (result i32) i32.const 13 ref.i31 i32.const 2 array.new $r ref.cast (ref $r) i32.const 0 array.get $r ref.cast (ref i31) i31.get_u)
(func (export "float") (result i64) f64.const 9 i32.const 2 array.new $f ref.cast (ref $f) i32.const 1 array.get $f i64.reinterpret_f64)
(func (export "vector") (result i32) v128.const i32x4 11 12 13 14 i32.const 2 array.new $v ref.cast (ref $v) i32.const 1 array.get $v i32x4.extract_lane 2)
(func (export "loop") (result i32) (local $a eqref) (local $i i32) (local $sum i32) global.get $array local.set $a i32.const 8192 local.set $i (loop $loop
local.get $sum local.get $a ref.cast (ref $a)
i32.const 1
array.get $a i32.add local.set $sum local.get $i i32.const 1 i32.sub local.tee $i br_if $loop) local.get $sum)
(func (export "pressure") (result i32) (local $a eqref) (local $i i32) (local $sum i32) global.get $array local.set $a i32.const 8192 local.set $i (loop $loop
global.get $g0 i64.const 1 i64.add global.set $g0
global.get $g1 i64.const 2 i64.add global.set $g1
global.get $g2 i64.const 3 i64.add global.set $g2
global.get $g3 i64.const 4 i64.add global.set $g3
global.get $g4 i64.const 5 i64.add global.set $g4
global.get $g5 i64.const 6 i64.add global.set $g5
global.get $g6 i64.const 7 i64.add global.set $g6
global.get $g7 i64.const 8 i64.add global.set $g7
local.get $sum local.get $a ref.cast (ref $a)
i32.const 1
array.get $a i32.add local.set $sum local.get $i i32.const 1 i32.sub local.tee $i br_if $loop) local.get $sum)
(func (export "negative-nop") (result i32) (local $a eqref) (local $i i32) (local $sum i32) global.get $array local.set $a i32.const 8192 local.set $i (loop $loop
local.get $sum local.get $a ref.cast (ref $a)
nop
i32.const 1
array.get $a i32.add local.set $sum local.get $i i32.const 1 i32.sub local.tee $i br_if $loop) local.get $sum)
(func (export "negative-index") (result i32) (local $a eqref) (local $i i32) (local $sum i32) global.get $array local.set $a i32.const 8192 local.set $i (loop $loop
local.get $sum local.get $a ref.cast (ref $a)
i32.const 1
i32.const 0 i32.add
array.get $a i32.add local.set $sum local.get $i i32.const 1 i32.sub local.tee $i br_if $loop) local.get $sum)
(func (export "pin-entry") (result i32) (local $a eqref) global.get $array local.set $a (loop
global.get $g0 i64.const 1 i64.add global.set $g0
global.get $g1 i64.const 2 i64.add global.set $g1
global.get $g2 i64.const 3 i64.add global.set $g2
global.get $g3 i64.const 4 i64.add global.set $g3
global.get $g4 i64.const 5 i64.add global.set $g4
global.get $g5 i64.const 6 i64.add global.set $g5
global.get $g6 i64.const 7 i64.add global.set $g6
global.get $g7 i64.const 8 i64.add global.set $g7
) local.get $a ref.cast (ref $a) i32.const 1 array.get $a)
(func (export "index-trap") (result i32) global.get $array ref.cast (ref $a) i32.const 1 i32.const 0 i32.div_u array.get $a)
(func (export "roots") (result i32) (local $saved eqref) struct.new $s i32.const 2 array.new $r ref.cast (ref $r) i32.const 0 array.get $r local.set $saved i32.const 32 array.new_default $a drop local.get $saved ref.cast (ref $s) ref.is_null)
(func (export "ref-loop") (result i32) (local $a eqref) (local $i i32) (local $sum i32)
 global.get $refs local.set $a i32.const 8192 local.set $i
 (loop $loop local.get $sum local.get $a ref.cast (ref $r) i32.const 1 array.get $r ref.is_null i32.eqz i32.add local.set $sum
 local.get $i i32.const 1 i32.sub local.tee $i br_if $loop) local.get $sum)
(func (export "float32") (result i32) f32.const 9 i32.const 2 array.new $f32 ref.cast (ref $f32) i32.const 1 array.get $f32 i32.reinterpret_f32)
(func (export "int64") (result i64) i64.const 1234567890123 i32.const 2 array.new $i64 ref.cast (ref $i64) i32.const 1 array.get $i64)
(func (export "nonfinal") (result i32) i32.const 17 i32.const 2 array.new $open ref.cast (ref $open) i32.const 1 array.get $open)
)
