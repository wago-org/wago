(module
 (type $cell (struct (field (mut i32))))
 (type $empty (struct))
 (type $array (array (mut (ref null $cell))))
 (type $holder (struct (field (ref $array)) (field i32)))
 (type $cell-call (func (param (ref $cell)) (result (ref $cell))))
 (type $pair-call (func (param (ref $holder) i32) (result (ref null $cell))))
 (global $escape (mut eqref) (ref.null eq))
 (global $effects (mut i32) (i32.const 0))
 (func $cell-target (type $cell-call)
  global.get $effects i32.const 1 i32.add global.set $effects
  struct.new $empty global.set $escape local.get 0)
 (func $cell-forward (type $cell-call) local.get 0 ref.cast (ref $cell) call $cell-target)
 (func $cell-middle (type $cell-call) local.get 0 call $cell-forward)
 (func (export "run") (result i32) (local $sum i32)
  i32.const 17 struct.new $cell i32.const 23 struct.new $cell call $cell-middle
  struct.new $empty global.set $escape
  struct.get $cell 0 local.set $sum struct.get $cell 0 local.get $sum i32.add)
 (func $pair-target (type $pair-call)
  local.get 1 local.get 0 ref.cast (ref $holder) struct.get $holder 1 i32.ge_u
  if (result (ref null $cell)) unreachable else
   local.get 0 ref.cast (ref $holder) struct.get $holder 0 ref.cast (ref $array)
   local.get 1 array.get $array
  end)
 (func $pair-forward (type $pair-call) local.get 0 ref.cast (ref $holder) local.get 1 call $pair-target)
 (func (export "pair") (result i32) (local $sum i32)
  i32.const 17 struct.new $cell i32.const 23 struct.new $cell array.new_fixed $array 1
  i32.const 1 struct.new $holder i32.const 0 call $pair-forward
  struct.new $empty global.set $escape
  ref.as_non_null struct.get $cell 0 local.set $sum struct.get $cell 0 local.get $sum i32.add)
 (func $null-target (param (ref null $cell)) (result i32) i32.const 7)
 (func $null-forward (param (ref null $cell)) (result i32)
  local.get 0 ref.cast (ref null $cell) call $null-target)
 (func (export "null") (result i32) ref.null $cell call $null-forward)
 (func $dynamic (param eqref) (result (ref $cell)) local.get 0 ref.cast (ref $cell) call $cell-target)
 (func (export "trap-null") (result i32) ref.null eq call $dynamic struct.get $cell 0)
 (func (export "trap-type") (result i32) struct.new $empty call $dynamic struct.get $cell 0)
 (func (export "reset") i32.const 0 global.set $effects)
 (func (export "effects") (result i32) global.get $effects)
 (func $assigned (type $cell-call)
  i32.const 91 struct.new $cell local.set 0 local.get 0 ref.cast (ref $cell) call $cell-target)
 (func (export "assigned") (result i32)
  i32.const 23 struct.new $cell call $assigned struct.get $cell 0)

 (func $chain1 (type $cell-call) local.get 0 ref.cast (ref $cell) call $cell-forward)
 (func $chain2 (type $cell-call) local.get 0 ref.cast (ref $cell) call $chain1)
 (func $chain3 (type $cell-call) local.get 0 ref.cast (ref $cell) call $chain2)
 (func $chain4 (type $cell-call) local.get 0 ref.cast (ref $cell) call $chain3)
 (func $chain5 (type $cell-call) local.get 0 ref.cast (ref $cell) call $chain4)
 (func $chain6 (type $cell-call) local.get 0 ref.cast (ref $cell) call $chain5)
 (func (export "chain") (result i32) (local $sum i32)
  i32.const 17 struct.new $cell i32.const 23 struct.new $cell call $chain6
  struct.new $empty global.set $escape struct.get $cell 0 local.set $sum
  struct.get $cell 0 local.get $sum i32.add)
 (func $cycle-a (type $cell-call) local.get 0 ref.cast (ref $cell) call $cycle-b)
 (func $cycle-b (type $cell-call) local.get 0 ref.cast (ref $cell) call $cycle-a)
 (func (export "cycle") (result i32) i32.const 1 struct.new $cell call $cycle-a struct.get $cell 0)
)
