(module
(type $s (struct (field (mut eqref)) (field (mut i32)) (field (mut i32))))
(type $open (sub (struct (field eqref) (field i32))))
(type $empty (struct))
(type $guard-result (func (result i32)))
(type $cell (struct (field i32)))
(type $packed (struct (field i8) (field i32)))
(memory 1)
(global $root (mut eqref) (ref.null eq))
(global $dirty (mut i64) (i64.const 0))
(global $allocation (mut eqref) (ref.null eq))
(func $cmp (param i32 i32) (result i32) local.get 0 local.get 1 i32.ge_u)
(func $notleaf (param i32 i32) (result i32) global.get $dirty drop local.get 0 local.get 1 i32.ge_u)
(func (export "init") i32.const 17 struct.new $cell i32.const 4 i32.const 9 struct.new $s global.set $root)
(func (export "clear") ref.null eq global.set $root)
(func (export "wrong") struct.new $empty global.set $root)
(func $read (export "read") (param $index i32) (result eqref) (local $obj eqref)
 global.get $root local.set $obj 
 local.get $index local.get $obj ref.cast (ref $s) struct.get $s 1
  call $cmp
 if (result eqref) unreachable unreachable else
 local.get $obj ref.cast (ref $s) struct.get $s 0
 end )
(func (export "nullable") (param $index i32) (result eqref) (local $obj eqref)
 global.get $root local.set $obj 
 local.get $index local.get $obj ref.cast (ref null $s) struct.get $s 1
  call $cmp
 if (result eqref) unreachable unreachable else
 local.get $obj ref.cast (ref null $s) struct.get $s 0
 end )
(func $noninline (export "noninline") (param $index i32) (result eqref) (local $obj eqref)
 global.get $root local.set $obj 
 local.get $index local.get $obj ref.cast (ref $s) struct.get $s 1
  call $notleaf
 if (result eqref) unreachable unreachable else
 local.get $obj ref.cast (ref $s) struct.get $s 0
 end )
(func (export "store") (param $index i32) (result eqref) (local $obj eqref)
 global.get $root local.set $obj 
 local.get $index local.get $obj ref.cast (ref $s) struct.get $s 1
 i32.const 0 i32.const 1 i32.store call $cmp
 if (result eqref) unreachable unreachable else
 local.get $obj ref.cast (ref $s) struct.get $s 0
 end )
(func (export "local-write") (param $index i32) (result eqref) (local $obj eqref)
 global.get $root local.set $obj 
 local.get $index local.get $obj ref.cast (ref $s) struct.get $s 1
 local.get $obj local.set $obj call $cmp
 if (result eqref) unreachable unreachable else
 local.get $obj ref.cast (ref $s) struct.get $s 0
 end )
(func $both (export "both-arms") (param $index i32) (result eqref) (local $obj eqref)
 global.get $root local.set $obj 
 local.get $index local.get $obj ref.cast (ref $s) struct.get $s 1
  call $cmp
 if (result eqref) ref.null eq else
 local.get $obj ref.cast (ref $s) struct.get $s 0
 end )
(func (export "larger") (param $index i32) (result i32) (local $obj eqref)
 global.get $root local.set $obj 
 local.get $index local.get $obj ref.cast (ref $s) struct.get $s 1
  call $cmp
 if (result i32) unreachable unreachable else
 local.get $obj ref.cast (ref $s) struct.get $s 2
 end )
(func (export "different-type") (param $index i32) (result eqref) (local $obj eqref)
 global.get $root local.set $obj 
 local.get $index local.get $obj ref.cast (ref $s) struct.get $s 1
  call $cmp
 if (result eqref) unreachable unreachable else
 local.get $obj ref.cast (ref $open) struct.get $open 0
 end )
(func (export "budget") (param $index i32) (result eqref) (local $obj eqref)
 global.get $root local.set $obj 
 local.get $index local.get $obj ref.cast (ref $s) struct.get $s 1
  call $cmp
 if (result eqref) unreachable nop nop nop nop nop nop nop nop nop nop nop nop nop nop nop nop nop nop nop nop nop nop nop nop nop nop nop nop nop nop nop nop nop nop  else
 local.get $obj ref.cast (ref $s) struct.get $s 0
 end )
(func (export "roots") (param i32) (result i32) (local $obj eqref) (local $saved eqref)
 global.get $root local.set $obj
 local.get 0 local.get $obj ref.cast (ref $s) struct.get $s 1 call $cmp
 if (result eqref) unreachable else local.get $obj ref.cast (ref $s) struct.get $s 0 end
 local.set $saved struct.new $empty global.set $allocation
 local.get $saved ref.cast (ref $cell) struct.get $cell 0)
(func (export "dirty") (param $index i32) (result eqref) (local $obj eqref)
 global.get $root local.set $obj global.get $dirty i64.const 1 i64.add global.set $dirty
 local.get $index local.get $obj ref.cast (ref $s) struct.get $s 1
  call $cmp
 if (result eqref) unreachable unreachable else
 local.get $obj ref.cast (ref $s) struct.get $s 0
 end )
(func (export "dirty-value") (result i64) global.get $dirty)
(func (export "pressure") (param $index i32) (param i64 i64 i64 i64 i64 i64 i64 i64) (result i64) (local $obj eqref)
 global.get $root local.set $obj
 local.get $index local.get $obj ref.cast (ref $s) struct.get $s 1 call $cmp
 if (result eqref) unreachable else local.get $obj ref.cast (ref $s) struct.get $s 0 end drop
 local.get 1 local.get 2 i64.add local.get 3 i64.add local.get 4 i64.add
 local.get 5 i64.add local.get 6 i64.add local.get 7 i64.add local.get 8 i64.add)
(func (export "subtype") (param $index i32) (result eqref) (local $obj eqref)
 global.get $root local.set $obj
 local.get $index local.get $obj ref.cast (ref $open) struct.get $open 1 call $cmp
 if (result eqref) unreachable else local.get $obj ref.cast (ref $open) struct.get $open 0 end)
(func (export "loop") (result i32) (local $i i32) (local $sum i32)
 i32.const 8192 local.set $i
 (loop $loop local.get $sum i32.const 1 call $read ref.cast (ref $cell) struct.get $cell 0 i32.add local.set $sum
 local.get $i i32.const 1 i32.sub local.tee $i br_if $loop) local.get $sum)
(func (export "negative-loop") (result i32) (local $i i32) (local $sum i32)
 i32.const 8192 local.set $i
 (loop $loop local.get $sum i32.const 1 call $both ref.cast (ref $cell) struct.get $cell 0 i32.add local.set $sum
 local.get $i i32.const 1 i32.sub local.tee $i br_if $loop) local.get $sum)
(func (export "noninline-loop") (result i32) (local $i i32) (local $sum i32)
 i32.const 8192 local.set $i
 (loop $loop local.get $sum i32.const 1 call $noninline ref.cast (ref $cell) struct.get $cell 0 i32.add local.set $sum
 local.get $i i32.const 1 i32.sub local.tee $i br_if $loop) local.get $sum)
(func (export "same-field") (param $index i32) (result i32) (local $obj eqref)
 global.get $root local.set $obj
 local.get $index local.get $obj ref.cast (ref $s) struct.get $s 1 call $cmp
 if (result i32) unreachable else local.get $obj ref.cast (ref $s) struct.get $s 1 end)
(func (export "packed-s") (param $index i32) (result i32) (local $obj eqref)
 i32.const -2 i32.const 4 struct.new $packed local.set $obj
 local.get $index local.get $obj ref.cast (ref $packed) struct.get $packed 1 call $cmp
 if (result i32) unreachable else local.get $obj ref.cast (ref $packed) struct.get_s $packed 0 end)
(func (export "packed-u") (param $index i32) (result i32) (local $obj eqref)
 i32.const -2 i32.const 4 struct.new $packed local.set $obj
 local.get $index local.get $obj ref.cast (ref $packed) struct.get $packed 1 call $cmp
 if (result i32) unreachable else local.get $obj ref.cast (ref $packed) struct.get_u $packed 0 end)
(func (export "indexed-block") (param $index i32) (result i32) (local $obj eqref)
 global.get $root local.set $obj
 local.get $index local.get $obj ref.cast (ref $s) struct.get $s 1 call $cmp
 if (type $guard-result) unreachable else local.get $obj ref.cast (ref $s) struct.get $s 1 end)
(func (export "block-param") (param $index i32) (result eqref) (local $obj eqref)
 global.get $root local.set $obj i32.const 99
 local.get $index local.get $obj ref.cast (ref $s) struct.get $s 1 call $cmp
 if (param i32) (result eqref) unreachable else drop local.get $obj ref.cast (ref $s) struct.get $s 0 end)
(func (export "block-multi") (param $index i32) (result eqref i32) (local $obj eqref)
 global.get $root local.set $obj
 local.get $index local.get $obj ref.cast (ref $s) struct.get $s 1 call $cmp
 if (result eqref i32) unreachable else local.get $obj ref.cast (ref $s) struct.get $s 0 i32.const 1 end)
)
