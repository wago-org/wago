(module
  (type $super (sub (func)))
  (type $sub (sub $super (func)))
  (type $callSuper (sub (func (param i32) (result i32))))
  (type $callSub (sub $callSuper (func (param i32) (result i32))))
  (type $other (func (param i64)))

  (import "env" "f" (func $import (type $super)))

  (table 5 funcref)
  (elem (i32.const 0) func $import $local16 $echo)
  (elem (i32.const 4) func $otherFunc)

  (func $local0 (type $sub))
  (func $local1 (type $sub))
  (func $local2 (type $sub))
  (func $local3 (type $sub))
  (func $local4 (type $sub))
  (func $local5 (type $sub))
  (func $local6 (type $sub))
  (func $local7 (type $sub))
  (func $local8 (type $sub))
  (func $local9 (type $sub))
  (func $local10 (type $sub))
  (func $local11 (type $sub))
  (func $local12 (type $sub))
  (func $local13 (type $sub))
  (func $local14 (type $sub))
  (func $local15 (type $sub))
  (func $local16 (type $sub))
  (func $echo (type $callSub) (param i32) (result i32)
    (i32.add (local.get 0) (i32.const 1)))
  (func $otherFunc (type $other) (param i64))

  (func (export "cast_import") (result i32)
    (drop (ref.cast (ref $super) (table.get 0 (i32.const 0))))
    (i32.const 1))

  (func (export "cast_local_after_imports_and_limit") (result i32)
    (drop (ref.cast (ref $super) (table.get 0 (i32.const 1))))
    (i32.const 1))

  (func (export "call_import") (result i32)
    (call_indirect (type $super) (i32.const 0))
    (i32.const 1))

  (func (export "call_local_after_imports_and_limit") (result i32)
    (call_indirect (type $super) (i32.const 1))
    (i32.const 1))

  (func (export "call_with_arguments") (result i32)
    (call_indirect (type $callSuper) (i32.const 41) (i32.const 2)))

  (func (export "cast_null") (result i32)
    (ref.is_null (ref.cast (ref null $super) (table.get 0 (i32.const 3)))))

  (func (export "cast_null_nonnullable")
    (drop (ref.cast (ref $super) (table.get 0 (i32.const 3)))))

  (func (export "cast_unrelated")
    (drop (ref.cast (ref $super) (table.get 0 (i32.const 4)))))
)
