(module
 (type $cell (struct (field i32)))
 (type $empty (struct))
 (type $signature (func (param eqref) (result eqref)))
 (global $allocation (mut eqref) (ref.null eq))
 (func $target (type $signature)
  struct.new $empty global.set $allocation local.get 0)
 (func $source (type $signature) local.get 0 return_call $target)
 (func $middle (type $signature) local.get 0 call $source)
 (func $exercise (result i32) (local $sum i32)
  i32.const 17 struct.new $cell
  i32.const 23 struct.new $cell call $middle ref.cast (ref $cell) struct.get $cell 0 local.set $sum
  ref.cast (ref $cell) struct.get $cell 0 local.get $sum i32.add)
 (func (export "run") (result i32) call $exercise)

 (func $source-indirect (type $signature) local.get 0 i32.const 0 return_call_indirect (type $signature))
 (func $middle-indirect (type $signature) local.get 0 call $source-indirect)
 (func $exercise-indirect (result i32) (local $sum i32)
  i32.const 17 struct.new $cell
  i32.const 23 struct.new $cell call $middle-indirect ref.cast (ref $cell) struct.get $cell 0 local.set $sum
  ref.cast (ref $cell) struct.get $cell 0 local.get $sum i32.add)
 (func (export "run-indirect") (result i32) call $exercise-indirect)

 (func $source-reference (type $signature) local.get 0 ref.func $target return_call_ref $signature)
 (func $middle-reference (type $signature) local.get 0 call $source-reference)
 (func $exercise-reference (result i32) (local $sum i32)
  i32.const 17 struct.new $cell
  i32.const 23 struct.new $cell call $middle-reference ref.cast (ref $cell) struct.get $cell 0 local.set $sum
  ref.cast (ref $cell) struct.get $cell 0 local.get $sum i32.add)
 (func (export "run-reference") (result i32) call $exercise-reference)
 (table 1 funcref)
 (elem (i32.const 0) func $target)

 (func (export "touch-table") i32.const 0 i32.const 0 table.get 0 table.set 0 i32.const 0 i32.const 0 table.get $numeric-table table.set $numeric-table)
 (type $pair (func (param eqref) (result eqref eqref)))
 (func $target-pair (type $pair) struct.new $empty global.set $allocation local.get 0 local.get 0)
 (func $source-pair (type $pair) local.get 0 ref.func $target-pair return_call_ref $pair)
 (func $middle-pair (type $pair) local.get 0 call $source-pair)
 (func (export "run-pair") (result i32) (local $sum i32)
  i32.const 17 struct.new $cell i32.const 23 struct.new $cell call $middle-pair
  ref.cast (ref $cell) struct.get $cell 0 local.set $sum
  ref.cast (ref $cell) struct.get $cell 0 local.get $sum i32.add local.set $sum
  ref.cast (ref $cell) struct.get $cell 0 local.get $sum i32.add)
 (elem declare func $target-pair)
 (type $numeric (func (param i32) (result eqref)))
 (func $target-numeric (type $numeric) (local $saved eqref)
  local.get 0 struct.new $cell local.set $saved struct.new $empty global.set $allocation local.get $saved)
 (func $source-numeric (type $numeric) local.get 0 ref.func $target-numeric return_call_ref $numeric)
 (func (export "run-numeric") (result i32) (local $sum i32)
  i32.const 17 struct.new $cell i32.const 23 call $source-numeric
  ref.cast (ref $cell) struct.get $cell 0 local.set $sum
  ref.cast (ref $cell) struct.get $cell 0 local.get $sum i32.add)
 (elem declare func $target-numeric)

 (func $source-numeric-direct (type $numeric) local.get 0 return_call $target-numeric)
 (table $numeric-table 1 funcref)
 (elem (table $numeric-table) (i32.const 0) func $target-numeric)
 (func $source-numeric-indirect (type $numeric) local.get 0 i32.const 0 return_call_indirect $numeric-table (type $numeric))
 (func (export "run-numeric-direct") (result i32) (local $sum i32)
  i32.const 17 struct.new $cell i32.const 23 call $source-numeric-direct
  ref.cast (ref $cell) struct.get $cell 0 local.set $sum
  ref.cast (ref $cell) struct.get $cell 0 local.get $sum i32.add)
 (func (export "run-numeric-indirect") (result i32) (local $sum i32)
  i32.const 17 struct.new $cell i32.const 23 call $source-numeric-indirect
  ref.cast (ref $cell) struct.get $cell 0 local.set $sum
  ref.cast (ref $cell) struct.get $cell 0 local.get $sum i32.add)
)
