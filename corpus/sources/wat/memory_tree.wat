;; memory_tree: recursive call tree where every node churns linear memory.
;; Stresses the combined regression surface: internal calls + stack frames +
;; repeated load/store traffic in a non-leaf body.
(module
  (memory 1)

  (func $mix (param $depth i32) (param $seed i32) (param $iters i32) (result i64)
    (local $i i32)
    (local $ptr i32)
    (local $acc i64)

    ;; Keep all accesses inside the first 64 KiB page and 8-byte aligned.
    (local.set $ptr
      (i32.shl
        (i32.and (local.get $seed) (i32.const 8191))
        (i32.const 3)))
    (local.set $acc
      (i64.extend_i32_u
        (i32.add
          (i32.mul (local.get $seed) (i32.const 1103515245))
          (local.get $depth))))
    (local.set $i (local.get $iters))

    (block $done
      (loop $loop
        (br_if $done (i32.eqz (local.get $i)))
        (i64.store
          (local.get $ptr)
          (i64.xor
            (local.get $acc)
            (i64.extend_i32_u
              (i32.add (local.get $seed) (local.get $i)))))
        (local.set $acc
          (i64.add
            (local.get $acc)
            (i64.load (local.get $ptr))))
        (local.set $ptr
          (i32.and
            (i32.add (local.get $ptr) (i32.const 64))
            (i32.const 65528)))
        (local.set $i (i32.sub (local.get $i) (i32.const 1)))
        (br $loop)))

    (if (result i64)
      (i32.eqz (local.get $depth))
      (then
        (local.get $acc))
      (else
        (i64.add
          (local.get $acc)
          (i64.add
            (call $mix
              (i32.sub (local.get $depth) (i32.const 1))
              (i32.add (local.get $seed) (i32.const 17))
              (local.get $iters))
            (call $mix
              (i32.sub (local.get $depth) (i32.const 1))
              (i32.add (local.get $seed) (i32.const 31))
              (local.get $iters)))))))

  (func (export "run") (param $depth i32) (param $iters i32) (result i64)
    (call $mix
      (local.get $depth)
      (i32.const 1)
      (local.get $iters))))
