(module
  (type (;0;) (func (param i32) (result i32)))
  (func (;0;) (type 0) (param i32) (result i32)
    (local i32 i32 i32 i32)
    block  ;; label = @1
      block  ;; label = @2
        local.get 0
        br_if 0 (;@2;)
        i32.const -2128831035
        local.set 1
        br 1 (;@1;)
      end
      i32.const 0
      local.set 2
      i32.const -2128831035
      local.set 1
      loop  ;; label = @2
        local.get 2
        i32.const 1
        i32.shl
        local.tee 3
        i32.const -1640531526
        i32.add
        local.tee 4
        i32.const 16
        i32.shr_u
        local.get 4
        i32.xor
        i32.const -2048144789
        i32.mul
        local.tee 4
        i32.const 13
        i32.shr_u
        local.get 4
        i32.xor
        i32.const 65535
        i32.and
        i32.const 1
        i32.add
        local.set 4
        local.get 3
        i32.const -1640531527
        i32.add
        local.tee 3
        i32.const 16
        i32.shr_u
        local.get 3
        i32.xor
        i32.const -2048144789
        i32.mul
        local.tee 3
        i32.const 13
        i32.shr_u
        local.get 3
        i32.xor
        i32.const 65535
        i32.and
        i32.const 1
        i32.add
        local.set 3
        loop  ;; label = @3
          local.get 3
          local.get 4
          local.tee 3
          i32.rem_u
          local.tee 4
          br_if 0 (;@3;)
        end
        local.get 3
        local.get 1
        i32.xor
        i32.const 16777619
        i32.mul
        local.set 1
        local.get 2
        i32.const 1
        i32.add
        local.tee 2
        local.get 0
        i32.ne
        br_if 0 (;@2;)
      end
    end
    local.get 1)
  (memory (;0;) 1 36)
  (global (;0;) (mut i32) (i32.const 65536))
  (export "memory" (memory 0))
  (export "benchmark" (func 0)))
