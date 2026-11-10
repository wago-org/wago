;; dispatch: call_indirect through a function table.
(module
  (type $bin (func (param i32 i32) (result i32)))
  (table 4 funcref)
  (elem (i32.const 0) $add $sub $mul $xor)
  (func $add (param i32 i32) (result i32) (i32.add (local.get 0) (local.get 1)))
  (func $sub (param i32 i32) (result i32) (i32.sub (local.get 0) (local.get 1)))
  (func $mul (param i32 i32) (result i32) (i32.mul (local.get 0) (local.get 1)))
  (func $xor (param i32 i32) (result i32) (i32.xor (local.get 0) (local.get 1)))
  (func (export "apply") (param i32 i32 i32) (result i32)
    ;; apply(which, a, b) = table[which](a, b)
    (call_indirect (type $bin) (local.get 1) (local.get 2) (local.get 0))))
