(module
  (type $super (sub (func)))
  (type $sub (sub $super (func)))

  (func $f (export "f") (type $sub))
  (func (export "get") (result (ref $sub))
    (ref.func $f))
  (elem declare func $f)
)
