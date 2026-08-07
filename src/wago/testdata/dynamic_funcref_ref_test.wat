(module
  (type $direct (func (param i32) (result i32)))
  (type $envfirst (func (param i32 i32) (result i32)))
  (type $closure-base (sub (struct (field (ref func)))))
  (type $closure (sub final $closure-base (struct (field (ref func)) (field i32))))
  (type $dispatch (func (param eqref i32) (result i32)))
  (type $run (func (param i32) (result i32)))
  (type $root (sub (func)))
  (type $child (sub final $root (func)))
  (type $unrelated (sub final (func)))
  (type $box (struct (field (ref func))))
  (type $test (func (result i32)))
  (type $get-child (func (result (ref $child))))
  (type $get-unrelated (func (result (ref $unrelated))))
  (type $accept-func (func (param (ref func)) (result i32)))

  (global (mut eqref) (ref.null eq))
  (global (mut (ref null i31)) (ref.null i31))
  (global (mut structref) (ref.null struct))
  (global (mut arrayref) (ref.null array))

  (func $direct_fn (type $direct) (param $x i32) (result i32)
    local.get $x
    i32.const 1
    i32.add)

  (func $envfirst_fn (type $envfirst) (param $env i32) (param $x i32) (result i32)
    local.get $env
    local.get $x
    i32.add)

  (func $dispatch (type $dispatch) (param $closure eqref) (param $x i32) (result i32)
    (local $entry (ref func))
    local.get $closure
    ref.cast (ref $closure-base)
    struct.get $closure-base 0
    local.set $entry
    local.get $entry
    ref.test (ref $direct)
    if (result i32)
      local.get $x
      local.get $entry
      ref.cast (ref $direct)
      call_ref $direct
    else
      local.get $closure
      ref.cast (ref $closure)
      struct.get $closure 1
      local.get $x
      local.get $entry
      ref.cast (ref $envfirst)
      call_ref $envfirst
    end)

  (func (export "direct") (type $run) (param $x i32) (result i32)
    ref.func $direct_fn
    i32.const 99
    struct.new $closure
    local.get $x
    call $dispatch)

  (func (export "environment") (type $run) (param $x i32) (result i32)
    ref.func $envfirst_fn
    i32.const 10
    struct.new $closure
    local.get $x
    call $dispatch)

  (func $root_fn (type $root))
  (func $child_fn (type $child))
  (func $unrelated_fn (type $unrelated))

  (func (export "get_child") (type $get-child) (result (ref $child))
    ref.func $child_fn)

  (func (export "get_unrelated") (type $get-unrelated) (result (ref $unrelated))
    ref.func $unrelated_fn)

  (func (export "foreign_is_root") (type $accept-func) (param $entry (ref func)) (result i32)
    local.get $entry
    ref.test (ref $root))

  (func (export "concrete_is_base") (type $test) (result i32)
    ref.func $direct_fn
    i32.const 0
    struct.new $closure
    ref.cast (ref $closure-base)
    drop
    i32.const 1)

  (func (export "child_is_root") (type $test) (result i32)
    ref.func $child_fn
    struct.new $box
    struct.get $box 0
    ref.test (ref $root))

  (func (export "root_is_child") (type $test) (result i32)
    ref.func $root_fn
    struct.new $box
    struct.get $box 0
    ref.test (ref $child))

  (func (export "unrelated_is_root") (type $test) (result i32)
    ref.func $unrelated_fn
    struct.new $box
    struct.get $box 0
    ref.test (ref $root))

  (elem declare func $direct_fn $envfirst_fn $root_fn $child_fn $unrelated_fn)
)
