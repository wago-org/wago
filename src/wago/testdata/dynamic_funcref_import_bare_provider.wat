(module
  (type $super (sub (func)))
  (type $sub (sub $super (func)))

  ;; Deliberately no ref.func, table, or element use: the provider must remain
  ;; descriptor-free so the consumer owns the imported proxy descriptor.
  (func (export "f") (type $sub))
)
