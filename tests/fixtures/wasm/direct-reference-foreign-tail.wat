(module
(type $cell (struct (field (mut i32))))
(type $typed (func (param (ref null $cell)) (result (ref null $cell))))
(type $eq (func (param eqref) (result eqref)))
(import "provider" "retain" (func $target (type $typed)))
(elem declare func $target)
(func $source (type $eq) local.get 0 ref.cast (ref null $cell) ref.func $target return_call_ref $typed)
(func $middle (type $eq) local.get 0 call $source)
(func (export "run") (param i32) (result i32) (local $sum i32)
i32.const 17 struct.new $cell local.get 0 struct.new $cell call $middle
ref.cast (ref $cell) struct.get $cell 0 local.set $sum
struct.get $cell 0 local.get $sum i32.add))