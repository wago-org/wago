(module
 (type $Box (struct (field i32) (field i32)))
 (type $Parent (struct (field (mut eqref)) (field (mut eqref))))
 (global $a (mut eqref) (ref.null eq))
 (global $b (mut eqref) (ref.null eq))
 (global $p (mut eqref) (ref.null eq))
 (global $steps (mut i32) (i32.const 0))
 (func (export "init")
  i32.const 7 i32.const 11 struct.new $Box global.set $a
  i32.const 13 i32.const 17 struct.new $Box global.set $b
  global.get $a global.get $b struct.new $Parent global.set $p
  i32.const 0 global.set $steps)
 (func (export "steps") (result i32) global.get $steps)
 (func $mix (export "mix") (param eqref eqref) (result i32) (local i32)
  local.get 0 ref.cast (ref $Box) struct.get $Box 0
  local.get 1 ref.cast (ref $Box) struct.get $Box 0 i32.add
  local.get 0 ref.cast (ref $Box) struct.get $Box 1 i32.add
  local.get 1 ref.cast (ref $Box) struct.get $Box 1 i32.add
  local.get 0 ref.cast (ref $Box) struct.get $Box 0 i32.add
  local.get 1 ref.cast (ref $Box) struct.get $Box 0 i32.add)
 (func (export "run") (result i32) global.get $a global.get $b call $mix)
 (func $copy (param eqref) (result i32) (local eqref)
  local.get 0 local.set 1
  local.get 0 ref.cast (ref $Box) struct.get $Box 0
  local.get 1 ref.cast (ref $Box) struct.get $Box 1 i32.add)
 (func (export "copy") (result i32) global.get $a call $copy)
 (func $children (param eqref) (result i32) (local i32)
  local.get 0 ref.cast (ref $Parent) struct.get $Parent 0 ref.cast (ref $Box) struct.get $Box 0
  local.get 0 ref.cast (ref $Parent) struct.get $Parent 1 ref.cast (ref $Box) struct.get $Box 0 i32.add
  local.get 0 ref.cast (ref $Parent) struct.get $Parent 0 ref.cast (ref $Box) struct.get $Box 1 i32.add
  local.get 0 ref.cast (ref $Parent) struct.get $Parent 1 ref.cast (ref $Box) struct.get $Box 1 i32.add)
 (func (export "children") (result i32) global.get $p call $children)
 (func $typed-children (param (ref $Parent)) (result i32) (local i32)
  local.get 0 struct.get $Parent 0 ref.cast (ref $Box) struct.get $Box 0
  local.get 0 struct.get $Parent 0 ref.cast (ref $Box) struct.get $Box 1 i32.add)
 (func (export "typed-children") (result i32) global.get $p ref.cast (ref $Parent) call $typed-children)
 (func $test-read (param eqref) (result i32) (local i32)
  local.get 0 ref.test (ref $Box)
  if (result i32)
   local.get 0 ref.cast (ref $Box) struct.get $Box 0
   local.get 0 ref.cast (ref $Box) struct.get $Box 1 i32.add
   local.get 0 ref.cast (ref $Box) struct.get $Box 0 i32.add
  else i32.const 99 end)
 (func (export "test") (result i32) global.get $a call $test-read)
 (func (export "test-wrong") (result i32) global.get $p call $test-read)
 (func $snapshot (param eqref eqref) (result i32) (local i32)
  local.get 0 ref.cast (ref $Box) struct.get $Box 0 drop
  local.get 0 ref.cast (ref $Box) struct.get $Box 0 drop
  local.get 0
  local.get 1 local.set 0
  ref.cast (ref $Box) struct.get $Box 0
  local.get 0 ref.cast (ref $Box) struct.get $Box 0 i32.add)
 (func (export "snapshot") (result i32) global.get $a global.get $b call $snapshot)
 (func $later (param eqref i32) (result i32) (local i32)
  local.get 0 ref.cast (ref null $Box) struct.get $Box 0
  local.get 0 ref.cast (ref $Box) struct.get $Box 1 i32.add)
 (func (export "later") (result i32) global.get $a i32.const 0 call $later)
 (func $alloc (result eqref) i32.const 23 i32.const 29 struct.new $Box)
 (func $after-call (param eqref) (result i32) (local eqref)
  local.get 0 ref.cast (ref $Box) struct.get $Box 0 drop
  local.get 0 ref.cast (ref $Box) struct.get $Box 0 drop
  call $alloc local.set 1
  local.get 0 ref.cast (ref $Box) struct.get $Box 1
  local.get 1 ref.cast (ref $Box) struct.get $Box 0 i32.add)
 (func (export "after-call") (result i32) global.get $a call $after-call)
 (func $branch (param eqref eqref i32) (result i32) (local i32)
  local.get 0 ref.cast (ref $Box) struct.get $Box 0 drop
  local.get 0 ref.cast (ref $Box) struct.get $Box 0 drop
  local.get 2 if local.get 1 local.set 0 else local.get 0 local.set 0 end
  local.get 0 ref.cast (ref $Box) struct.get $Box 1)
 (func (export "branch") (param i32) (result i32) global.get $a global.get $b local.get 0 call $branch)
 (func $write (param eqref eqref) (result i32) (local i32)
  local.get 0 ref.cast (ref $Parent) struct.get $Parent 0 ref.cast (ref $Box) struct.get $Box 0 drop
  local.get 0 ref.cast (ref $Parent) struct.get $Parent 0 ref.cast (ref $Box) struct.get $Box 0 drop
  local.get 0 ref.cast (ref $Parent) local.get 1 struct.set $Parent 0
  local.get 0 ref.cast (ref $Parent) struct.get $Parent 0 ref.cast (ref $Box) struct.get $Box 1)
 (func (export "write") (result i32) global.get $p global.get $b call $write)
 (func (export "loop") (param i32) (result i32) (local $i i32) (local $sum i32)
  block $done loop $again
   local.get $i local.get 0 i32.ge_u br_if $done
   local.get $sum global.get $a global.get $b call $mix i32.add local.set $sum
   local.get $i i32.const 1 i32.add local.set $i br $again
  end end local.get $sum)

 (func $small (param eqref) (result i32) (local i32)
  local.get 0 ref.cast (ref $Box) struct.get $Box 0
  local.get 0 ref.cast (ref $Box) struct.get $Box 0 i32.add)
 (func (export "small") (result i32) global.get $a call $small)
 (func $pressure (param eqref eqref i32 i32 i32 i32 i32 i32 i32 i32) (result i32) (local i32)
  local.get 2 local.get 3 i32.add local.get 4 i32.add local.get 5 i32.add
  local.get 6 i32.add local.get 7 i32.add local.get 8 i32.add local.get 9 i32.add
  local.get 0 ref.test (ref $Box) if (result i32)
   local.get 0 ref.cast (ref $Box) struct.get $Box 0
   local.get 1 ref.cast (ref $Box) struct.get $Box 0 i32.add
   local.get 0 ref.cast (ref $Box) struct.get $Box 1 i32.add
   local.get 1 ref.cast (ref $Box) struct.get $Box 1 i32.add
  else i32.const 99 end i32.add)
 (func (export "pressure") (result i32)
  global.get $a global.get $b i32.const 1 i32.const 2 i32.const 3 i32.const 4
  i32.const 5 i32.const 6 i32.const 7 i32.const 8 call $pressure)
)
