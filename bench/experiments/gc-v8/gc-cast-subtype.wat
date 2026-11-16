(module (type $base (sub (struct (field i32)))) (type $child (sub $base (struct (field i32) (field i32))))
  (func $performance_kernel (export "benchmark") (param $n i32) (result i32)
    (local $i i32) (local $a i32)

    (loop $loop
      (local.set $a (i32.add (local.get $a) (struct.get $base 0 (ref.cast (ref $base) (struct.new $child (local.get $i) (i32.const 7))))))
      (local.set $i (i32.add (local.get $i) (i32.const 1)))
      (br_if $loop (i32.lt_u (local.get $i) (local.get $n))))
    (local.get $a))
 (func (export "performance") (param $n i32) (result i32)
  (local $iteration i32) (local $checksum i32)
  (loop $repeat
   (local.set $checksum (i32.add (local.get $checksum) (call $performance_kernel (local.get $n))))
   (local.set $iteration (i32.add (local.get $iteration) (i32.const 1)))
   (br_if $repeat (i32.lt_u (local.get $iteration) (i32.const 64))))
  (local.get $checksum)))
