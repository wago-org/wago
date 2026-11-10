(module
  (type $Cell (struct (field i32)))
  (type $Box (struct (field (ref $Cell))))
  (type $Pair (array (mut i32)))
  (global $last (mut (ref null $Cell)) (ref.null $Cell))
  (func (export "scalar-structs") (param $n i32) (result i32)
    (local $i i32) (local $sum i32)
    (block $done
      (loop $again
        (br_if $done (i32.ge_u (local.get $i) (local.get $n)))
        (global.set $last (struct.new $Cell (local.get $i)))
        (local.set $sum
          (i32.add (local.get $sum) (struct.get $Cell 0 (global.get $last))))
        (local.set $i (i32.add (local.get $i) (i32.const 1)))
        (br $again)))
    (local.get $sum))
  (func (export "structs") (param $n i32) (result i32)
    (local $i i32) (local $sum i32) (local $box (ref null $Box))
    (block $done
      (loop $again
        (br_if $done (i32.ge_u (local.get $i) (local.get $n)))
        (local.set $box (struct.new $Box (struct.new $Cell (local.get $i))))
        (local.set $sum
          (i32.add (local.get $sum)
            (struct.get $Cell 0 (struct.get $Box 0 (local.get $box)))))
        (local.set $i (i32.add (local.get $i) (i32.const 1)))
        (br $again)))
    (local.get $sum))
  (func (export "alternating") (param $n i32) (result i32)
    (local $i i32) (local $sum i32)
    (local $box (ref null $Box)) (local $pair (ref null $Pair))
    (block $done
      (loop $again
        (br_if $done (i32.ge_u (local.get $i) (local.get $n)))
        (local.set $box (struct.new $Box (struct.new $Cell (local.get $i))))
        (local.set $pair
          (array.new_fixed $Pair 2
            (local.get $i) (i32.add (local.get $i) (i32.const 1))))
        (local.set $sum
          (i32.add (local.get $sum)
            (i32.add
              (struct.get $Cell 0 (struct.get $Box 0 (local.get $box)))
              (array.get $Pair (local.get $pair) (i32.const 1)))))
        (local.set $i (i32.add (local.get $i) (i32.const 1)))
        (br $again)))
    (local.get $sum))
  (func (export "alternating-default") (param $n i32) (result i32)
    (local $i i32) (local $sum i32)
    (local $box (ref null $Box)) (local $pair (ref null $Pair))
    (block $done
      (loop $again
        (br_if $done (i32.ge_u (local.get $i) (local.get $n)))
        (local.set $box (struct.new $Box (struct.new $Cell (local.get $i))))
        (local.set $pair
          (array.new_default $Pair (i32.const 2)))
        (local.set $sum
          (i32.add (local.get $sum)
            (i32.add
              (struct.get $Cell 0 (struct.get $Box 0 (local.get $box)))
              (array.get $Pair (local.get $pair) (i32.const 1)))))
        (local.set $i (i32.add (local.get $i) (i32.const 1)))
        (br $again)))
    (local.get $sum))
  (func (export "alternating-fill") (param $n i32) (result i32)
    (local $i i32) (local $sum i32)
    (local $box (ref null $Box)) (local $pair (ref null $Pair))
    (block $done
      (loop $again
        (br_if $done (i32.ge_u (local.get $i) (local.get $n)))
        (local.set $box (struct.new $Box (struct.new $Cell (local.get $i))))
        (local.set $pair
          (array.new $Pair (local.get $i) (i32.const 2)))
        (local.set $sum
          (i32.add (local.get $sum)
            (i32.add
              (struct.get $Cell 0 (struct.get $Box 0 (local.get $box)))
              (array.get $Pair (local.get $pair) (i32.const 1)))))
        (local.set $i (i32.add (local.get $i) (i32.const 1)))
        (br $again)))
    (local.get $sum)))
