(module
  (type $leaf (struct (field i32)))
  (table 2 (ref null $leaf))
  (elem (i32.const 0) (ref $leaf)
    (struct.new $leaf (i32.const 11))
    (struct.new $leaf (i32.const 22)))
  (func (export "sum") (result i32)
    (i32.add
      (struct.get $leaf 0 (table.get 0 (i32.const 0)))
      (struct.get $leaf 0 (table.get 0 (i32.const 1)))))
)
