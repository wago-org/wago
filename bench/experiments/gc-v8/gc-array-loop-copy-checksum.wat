(module
  (type $array (array (mut i32)))
  (func $checksum (param $a (ref null $array)) (result i32)
    (local $j i32) (local $total i32)
    (loop $elements
      (local.set $total (i32.add (local.get $total)
        (i32.mul (i32.add (local.get $j) (i32.const 1)) (array.get $array (local.get $a) (local.get $j)))))
      (local.set $j (i32.add (local.get $j) (i32.const 1)))
      (br_if $elements (i32.lt_u (local.get $j) (i32.const 32))))
    (local.get $total))

  (func (export "array-copy-checksum") (param $n i32) (result i32)
    (local $source (ref null $array)) (local $target (ref null $array))
    (local $i i32) (local $j i32) (local $total i32)
    (local.set $source (array.new $array (i32.const 0) (i32.const 32)))
    (local.set $target (array.new $array (i32.const -1) (i32.const 32)))
    (loop $initialize
      (array.set $array (local.get $source) (local.get $j)
        (i32.add (i32.mul (local.get $j) (i32.const 5)) (i32.const 11)))
      (local.set $j (i32.add (local.get $j) (i32.const 1)))
      (br_if $initialize (i32.lt_u (local.get $j) (i32.const 32))))
    (block $done
      (br_if $done (i32.eqz (local.get $n)))
      (loop $again
        (array.set $array (local.get $source) (i32.and (local.get $i) (i32.const 31))
          (i32.add (i32.mul (local.get $i) (i32.const 3)) (i32.const 7)))
        (array.copy $array $array
    (local.get $target) (i32.const 0) (local.get $source) (i32.const 0) (i32.const 32))
        (local.set $total (i32.add (local.get $total) (call $checksum (local.get $target))))
        (local.set $i (i32.add (local.get $i) (i32.const 1)))
        (br_if $again (i32.lt_u (local.get $i) (local.get $n)))))
    (local.get $total))

  (func $performance_kernel (export "array-loop-copy-checksum") (param $n i32) (result i32)
    (local $source (ref null $array)) (local $target (ref null $array))
    (local $i i32) (local $j i32) (local $total i32)
    (local.set $source (array.new $array (i32.const 0) (i32.const 32)))
    (local.set $target (array.new $array (i32.const -1) (i32.const 32)))
    (loop $initialize
      (array.set $array (local.get $source) (local.get $j)
        (i32.add (i32.mul (local.get $j) (i32.const 5)) (i32.const 11)))
      (local.set $j (i32.add (local.get $j) (i32.const 1)))
      (br_if $initialize (i32.lt_u (local.get $j) (i32.const 32))))
    (block $done
      (br_if $done (i32.eqz (local.get $n)))
      (loop $again
        (array.set $array (local.get $source) (i32.and (local.get $i) (i32.const 31))
          (i32.add (i32.mul (local.get $i) (i32.const 3)) (i32.const 7)))
        (local.set $j (i32.const 0))
    (loop $copy
      (array.set $array (local.get $target) (local.get $j)
        (array.get $array (local.get $source) (local.get $j)))
      (local.set $j (i32.add (local.get $j) (i32.const 1)))
      (br_if $copy (i32.lt_u (local.get $j) (i32.const 32))))
        (local.set $total (i32.add (local.get $total) (call $checksum (local.get $target))))
        (local.set $i (i32.add (local.get $i) (i32.const 1)))
        (br_if $again (i32.lt_u (local.get $i) (local.get $n)))))
    (local.get $total))

 (func (export "performance") (param $n i32) (result i32)
  (local $iteration i32) (local $checksum i32)
  (loop $repeat
   (local.set $checksum (i32.add (local.get $checksum) (call $performance_kernel (local.get $n))))
   (local.set $iteration (i32.add (local.get $iteration) (i32.const 1)))
   (br_if $repeat (i32.lt_u (local.get $iteration) (i32.const 64))))
  (local.get $checksum)))
