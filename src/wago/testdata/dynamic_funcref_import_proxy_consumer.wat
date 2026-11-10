(module
  (type $super (sub (func)))
  (type $sub (sub $super (func)))
  (type $box (struct (field (ref func))))

  (import "env" "f" (func $import (type $super)))
  (elem declare func $import)

  (func (export "test_direct") (result i32)
    (ref.test (ref $sub) (ref.func $import)))

  (func (export "test_stored") (result i32)
    (ref.test (ref $sub)
      (struct.get $box 0
        (struct.new $box (ref.func $import)))))
)
