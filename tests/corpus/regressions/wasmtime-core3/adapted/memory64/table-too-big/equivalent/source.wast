;; Core-semantic projection of upstream memory64/table-too-big.wast.
;; Wasmtime expects a host allocation trap; Core permits table.grow to return -1.
(module
  (table i64 0 funcref)
  (func (export "grow") (param i64) (result i64)
    (table.grow 0 (ref.null func) (local.get 0)))
  (func (export "size") (result i64)
    (table.size 0))
)
(assert_return (invoke "grow" (i64.const 0x2000_0000_0000_0000)) (i64.const -1))
(assert_return (invoke "size") (i64.const 0))
