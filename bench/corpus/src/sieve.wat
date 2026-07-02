;; sieve: Sieve of Eratosthenes up to n, returning the prime count. A byte-per-
;; number flag array in linear memory (re-initialised each call) drives a mix of
;; sequential writes, strided composite marking, and data-dependent branches —
;; a real algorithm touching memory, ALU, and control flow together.
(module
  (memory 8) ;; 512 KiB: n up to ~500k
  (func (export "count") (param $n i32) (result i32)
    (local $i i32) (local $j i32) (local $count i32)
    ;; init flags[2..n) = 1 (prime); 0 and 1 stay 0 (memory starts zeroed but is
    ;; reused across calls, so clear the whole range explicitly).
    (local.set $i (i32.const 0))
    (block $doneInit
      (loop $init
        (br_if $doneInit (i32.ge_s (local.get $i) (local.get $n)))
        (i32.store8 (local.get $i) (i32.ge_s (local.get $i) (i32.const 2)))
        (local.set $i (i32.add (local.get $i) (i32.const 1)))
        (br $init)))
    ;; sieve
    (local.set $i (i32.const 2))
    (block $doneSieve
      (loop $outer
        (br_if $doneSieve (i32.ge_s (i32.mul (local.get $i) (local.get $i)) (local.get $n)))
        (if (i32.load8_u (local.get $i))
          (then
            (local.set $j (i32.mul (local.get $i) (local.get $i)))
            (block $doneMark
              (loop $mark
                (br_if $doneMark (i32.ge_s (local.get $j) (local.get $n)))
                (i32.store8 (local.get $j) (i32.const 0))
                (local.set $j (i32.add (local.get $j) (local.get $i)))
                (br $mark)))))
        (local.set $i (i32.add (local.get $i) (i32.const 1)))
        (br $outer)))
    ;; count survivors
    (local.set $i (i32.const 2))
    (block $doneCount
      (loop $countLoop
        (br_if $doneCount (i32.ge_s (local.get $i) (local.get $n)))
        (local.set $count (i32.add (local.get $count) (i32.load8_u (local.get $i))))
        (local.set $i (i32.add (local.get $i) (i32.const 1)))
        (br $countLoop)))
    (local.get $count)))
