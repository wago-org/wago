(module
 (type $Ints (array (mut i32)))
 (type $Refs (array (mut eqref)))
 (type $IntBox (struct (field (ref null $Ints)) (field i32)))
 (type $RefBox (struct (field (ref null $Refs)) (field i32)))
 (type $Cell (struct (field i32)))
 (type $Other (struct (field i64)))
 (global $ints (mut eqref) (ref.null eq))
 (global $refs (mut eqref) (ref.null eq))
 (global $allocation (mut eqref) (ref.null eq))
 (global $refs2 (mut eqref) (ref.null eq))
 (func $gte (param i32 i32) (result i32) local.get 0 local.get 1 i32.ge_u)
 (func $int-read (export "int-read") (param eqref i32) (result i32)
  local.get 1 local.get 0 ref.cast (ref $IntBox) struct.get $IntBox 1
  call $gte
  if (result i32)
   unreachable
  else
   local.get 0 ref.cast (ref $IntBox) struct.get $IntBox 0
   ref.cast (ref $Ints) local.get 1 array.get $Ints
  end)
 (func $ref-read (export "ref-read") (param eqref i32) (result eqref)
  local.get 1 local.get 0 ref.cast (ref $RefBox) struct.get $RefBox 1
  call $gte
  if (result eqref)
   unreachable
  else
   local.get 0 ref.cast (ref $RefBox) struct.get $RefBox 0
   ref.cast (ref $Refs) local.get 1 array.get $Refs
  end)
 (func $both (export "both") (param eqref i32) (result eqref)
  local.get 1 local.get 0 ref.cast (ref $RefBox) struct.get $RefBox 1
  call $gte
  if (result eqref)
   local.get 0 ref.cast (ref $RefBox) struct.get $RefBox 0
   ref.cast (ref $Refs) i32.const 1 array.get $Refs
  else
   local.get 0 ref.cast (ref $RefBox) struct.get $RefBox 0
   ref.cast (ref $Refs) i32.const 0 array.get $Refs
  end)
 (func (export "init")
  (global.set $ints (struct.new $IntBox (array.new $Ints (i32.const 7) (i32.const 16)) (i32.const 16)))
  (global.set $refs (struct.new $RefBox
   (array.new_fixed $Refs 2 (struct.new $Cell (i32.const 17)) (struct.new $Cell (i32.const 23)))
   (i32.const 2))))
 (func (export "init-second")
  (global.set $refs2 (struct.new $RefBox
   (array.new_fixed $Refs 2 (struct.new $Cell (i32.const 31)) (struct.new $Cell (i32.const 41))) (i32.const 2))))
 (func (export "wrong")
  (global.set $ints (struct.new $Other (i64.const 0))))
 (func (export "null") (global.set $ints (ref.null eq)))
 (func (export "missing")
  (global.set $ints (struct.new $IntBox (ref.null $Ints) (i32.const 16))))
 (func (export "short")
  (global.set $ints (struct.new $IntBox (array.new $Ints (i32.const 7) (i32.const 1)) (i32.const 16))))
 (func (export "read") (param i32) (result i32)
  global.get $ints local.get 0 call $int-read)
 (func (export "loop") (param $n i32) (result i32) (local $i i32) (local $sum i32)
  (block $done (loop $again
   (br_if $done (i32.ge_u (local.get $i) (local.get $n)))
   (local.set $sum (i32.add (local.get $sum)
    (call $int-read (global.get $ints) (i32.and (local.get $i) (i32.const 15)))))
   (local.set $i (i32.add (local.get $i) (i32.const 1))) (br $again)))
  local.get $sum)
 (func $allocate
  (global.set $allocation (struct.new $Cell (i32.const 99))))
 (func $value (param eqref) (result i32)
  local.get 0 ref.cast (ref $Cell) struct.get $Cell 0)
 (func (export "roots") (result i32) (local $independent eqref) (local $first eqref) (local $second eqref)
  (local.set $independent (struct.new $Cell (i32.const 1000)))
  call $allocate
  (local.set $first (call $ref-read (global.get $refs) (i32.const 0)))
  (local.set $second (call $ref-read (global.get $refs) (i32.const 1)))
  (global.set $refs (ref.null eq))
  call $allocate
  (i32.add (i32.add (call $value (local.get $first)) (call $value (local.get $second))) (call $value (local.get $independent))))
 (func (export "stack-roots") (result i32) (local $child i32)
  (struct.new $Cell (i32.const 1000))
  (call $ref-read (global.get $refs) (i32.const 0))
  (global.set $refs (ref.null eq)) call $allocate
  call $value local.set $child
  call $value local.get $child i32.add)
 (func (export "branch") (param i32) (result i32)
  (call $value (call $both (global.get $refs) (local.get 0))))

 (func $forward-one (param eqref i32) (result i32) local.get 0 local.get 1 call $int-read)
 (func $forward-two (param eqref i32) (result i32) local.get 0 local.get 1 call $forward-one)
 (func (export "forward") (param i32) (result i32) global.get $ints local.get 0 call $forward-two)
 (func $arms (param eqref i32) (result i32)
  local.get 1
  if (result i32)
   local.get 0 ref.cast (ref $IntBox) struct.get $IntBox 0
   ref.cast (ref $Ints) i32.const 0 array.get $Ints
  else
   local.get 0 ref.cast (ref $IntBox) struct.get $IntBox 1
  end
  local.get 0 ref.cast (ref $IntBox) struct.get $IntBox 1 i32.add)
 (func (export "arms") (param i32) (result i32) global.get $ints local.get 0 call $arms)
 (func $late (param eqref i32) (result i32)
  local.get 0 ref.cast (ref $IntBox) struct.get $IntBox 0
  ref.cast (ref $Ints) i32.const 0 array.get $Ints
  local.get 1 if (result i32) unreachable else
   local.get 0 ref.cast (ref $IntBox) struct.get $IntBox 1
  end i32.add)
 (func (export "late") (param i32) (result i32) global.get $ints local.get 0 call $late)
 (func $nested (param eqref i32) (result eqref)
  local.get 1 if (result eqref)
   local.get 1 i32.const 1 i32.eq if (result eqref)
    local.get 0 ref.cast (ref $RefBox) struct.get $RefBox 0 ref.cast (ref $Refs) i32.const 1 array.get $Refs
   else
    local.get 0 ref.cast (ref $RefBox) struct.get $RefBox 0 ref.cast (ref $Refs) i32.const 0 array.get $Refs
   end
  else
   local.get 0 ref.cast (ref $RefBox) struct.get $RefBox 0 ref.cast (ref $Refs) i32.const 0 array.get $Refs
  end)
 (func (export "nested") (param i32) (result i32) global.get $refs local.get 0 call $nested call $value)

 (func $join (param eqref i32) (result i32)
  local.get 0 ref.cast (ref $IntBox) struct.get $IntBox 1
  if (result i32) i32.const 3 else i32.const 5 end
  local.get 0 ref.cast (ref $IntBox) struct.get $IntBox 1 i32.add)
 (func (export "join") (result i32) global.get $ints i32.const 0 call $join)
 (func (export "zero") (global.set $ints (struct.new $IntBox (ref.null $Ints) (i32.const 0))))

 (func (export "pressure") (param i32 i32 i32 i32 i32 i32 i32 i32) (result i32)
  global.get $ints i32.const 0 call $int-read
  local.get 0 i32.add local.get 1 i32.add local.get 2 i32.add local.get 3 i32.add
  local.get 4 i32.add local.get 5 i32.add local.get 6 i32.add local.get 7 i32.add)
 (func (export "different") (result i32) (local $a eqref) (local $b eqref)
  global.get $refs i32.const 0 call $ref-read local.set $a
  global.get $refs2 i32.const 1 call $ref-read local.set $b
  ref.null eq global.set $refs ref.null eq global.set $refs2 call $allocate
  local.get $a call $value local.get $b call $value i32.add)
 (func (export "two-targets") (result i32)
  global.get $ints i32.const 0 call $int-read
  global.get $refs i32.const 0 call $ref-read call $value i32.add)

 (func (export "pressure-root") (param i32 i32 i32 i32 i32 i32 i32 i32) (result i32) (local $child eqref)
  global.get $refs i32.const 0 call $ref-read local.set $child
  ref.null eq global.set $refs call $allocate
  local.get $child call $value
  local.get 0 i32.add local.get 1 i32.add local.get 2 i32.add local.get 3 i32.add
  local.get 4 i32.add local.get 5 i32.add local.get 6 i32.add local.get 7 i32.add)
 (func (export "many") (result i32)
  global.get $ints i32.const 0 call $int-read
  global.get $ints i32.const 0 call $int-read i32.add
  global.get $ints i32.const 0 call $int-read i32.add
  global.get $ints i32.const 0 call $int-read i32.add
  global.get $ints i32.const 0 call $int-read i32.add
  global.get $ints i32.const 0 call $int-read i32.add
  global.get $ints i32.const 0 call $int-read i32.add
  global.get $ints i32.const 0 call $int-read i32.add
  global.get $ints i32.const 0 call $int-read i32.add
  global.get $ints i32.const 0 call $int-read i32.add
  global.get $ints i32.const 0 call $int-read i32.add
  global.get $ints i32.const 0 call $int-read i32.add
  global.get $ints i32.const 0 call $int-read i32.add
  global.get $ints i32.const 0 call $int-read i32.add
  global.get $ints i32.const 0 call $int-read i32.add
  global.get $ints i32.const 0 call $int-read i32.add
  global.get $ints i32.const 0 call $int-read i32.add
)

 (func $home-branch (param eqref eqref) (result i32)
  local.get 0 ref.cast (ref $IntBox) struct.get $IntBox 1
  if (result i32)
   i32.const 1 if (result i32)
    local.get 1 ref.cast (ref $IntBox) struct.get $IntBox 1
   else
    local.get 1 ref.cast (ref $IntBox) struct.get $IntBox 1
   end
  else
   local.get 1 ref.cast (ref $IntBox) struct.get $IntBox 1
  end
  local.get 0 ref.cast (ref $IntBox) struct.get $IntBox 1 i32.add)
 (func (export "home-branch") (param i32 i32 i32 i32 i32 i32 i32 i32) (result i32)
  global.get $ints (struct.new $IntBox (ref.null $Ints) (i32.const 99)) call $home-branch
  local.get 0 i32.add local.get 1 i32.add local.get 2 i32.add local.get 3 i32.add
  local.get 4 i32.add local.get 5 i32.add local.get 6 i32.add local.get 7 i32.add)

 (func $trap-read (param eqref) (result i32)
  local.get 0 ref.cast (ref $IntBox) struct.get $IntBox 1
  if (result i32) unreachable else unreachable end)
 (func (export "after-trap") (result i32)
  global.get $ints call $trap-read drop call $allocate i32.const 0)

 (func (export "pressure-loop") (param i32 i32 i32 i32 i32 i32 i32 i32 i32) (result i32) (local $i i32) (local $sum i32)
  (block $done (loop $again
   (br_if $done (i32.ge_u (local.get $i) (local.get 8)))
   (local.set $sum (i32.add (local.get $sum)
    (call $int-read (global.get $ints) (i32.and (local.get $i) (i32.const 15)))))
   (local.set $i (i32.add (local.get $i) (i32.const 1))) (br $again)))
  local.get $sum local.get 0 i32.add local.get 1 i32.add local.get 2 i32.add local.get 3 i32.add
  local.get 4 i32.add local.get 5 i32.add local.get 6 i32.add local.get 7 i32.add)
)
