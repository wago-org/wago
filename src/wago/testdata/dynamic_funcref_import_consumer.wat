(module
  (type $super (sub (func)))
  (type $sub (sub $super (func)))

  (import "env" "f" (func $import (type $super)))
  (table 1 funcref)
  (elem (i32.const 0) func $import)

  (func (export "test_super") (result i32)
    (ref.test (ref $super) (table.get 0 (i32.const 0))))

  (func (export "test_sub") (result i32)
    (ref.test (ref $sub) (table.get 0 (i32.const 0))))
)
