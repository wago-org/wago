(module
 (type $s (struct (field eqref) (field i32)))
 (import "host" "compare" (func $compare (param i32 i32) (result i32)))
 (func (export "read") (param eqref i32) (result eqref)
 local.get 1 local.get 0 ref.cast (ref $s) struct.get $s 1 call $compare
 if (result eqref) unreachable else local.get 0 ref.cast (ref $s) struct.get $s 0 end))
