;; linked_list: build a singly-linked list of n nodes in linear memory, then
;; traverse it summing values. The traversal is a dependent-load pointer chase
;; (each node's .next must be loaded before the next node's address is known) —
;; a memory-latency workload distinct from the sequential memory.sum loop.
;; Node layout: [value:i32 @+0, next:i32 @+4], stride 8. Address 0 is the null
;; sentinel, so node i lives at (i+1)*8 and the head is 8.
(module
  (memory 4) ;; 256 KiB: up to ~32k nodes
  (func (export "sum") (param $n i32) (result i32)
    (local $i i32) (local $addr i32) (local $sum i32) (local $p i32)
    ;; build: node i at (i+1)*8; next = (i+2)*8, or 0 (null) for the last node
    (block $built
      (loop $build
        (br_if $built (i32.ge_s (local.get $i) (local.get $n)))
        (local.set $addr (i32.mul (i32.add (local.get $i) (i32.const 1)) (i32.const 8)))
        (i32.store (local.get $addr) (local.get $i))
        (i32.store offset=4 (local.get $addr)
          (if (result i32) (i32.eq (i32.add (local.get $i) (i32.const 1)) (local.get $n))
            (then (i32.const 0))
            (else (i32.mul (i32.add (local.get $i) (i32.const 2)) (i32.const 8)))))
        (local.set $i (i32.add (local.get $i) (i32.const 1)))
        (br $build)))
    ;; traverse from the head until the null next pointer
    (local.set $p (i32.const 8))
    (block $end
      (loop $walk
        (br_if $end (i32.eqz (local.get $p)))
        (local.set $sum (i32.add (local.get $sum) (i32.load (local.get $p))))
        (local.set $p (i32.load offset=4 (local.get $p)))
        (br $walk)))
    (local.get $sum)))
