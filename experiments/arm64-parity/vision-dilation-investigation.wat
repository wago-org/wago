(module
  (type (;0;) (func (param i32) (result i32)))
  (func (;0;) (type 0) (param i32) (result i32)
    (local i32 i32 i32 i32 i32 i32 i32 i32 i32 i32 i32 i32)
    block  ;; label = @1
      local.get 0
      local.get 0
      i32.mul
      local.tee 1
      i32.eqz
      br_if 0 (;@1;)
      i32.const 0
      local.set 2
      block  ;; label = @2
        local.get 1
        i32.const 1
        i32.eq
        br_if 0 (;@2;)
        local.get 1
        i32.const 1
        i32.and
        local.set 3
        local.get 1
        i32.const -4
        i32.and
        local.set 4
        i32.const 0
        local.set 2
        i32.const 65536
        local.set 1
        loop  ;; label = @3
          local.get 1
          i32.const 4
          i32.add
          local.get 2
          i32.const -1640531526
          i32.add
          local.tee 5
          i32.const 16
          i32.shr_u
          local.get 5
          i32.xor
          i32.const -2048144789
          i32.mul
          local.tee 5
          i32.const 13
          i32.shr_u
          local.get 5
          i32.xor
          i32.const 1
          i32.and
          i32.store
          local.get 1
          local.get 2
          i32.const -1640531527
          i32.add
          local.tee 5
          i32.const 16
          i32.shr_u
          local.get 5
          i32.xor
          i32.const -2048144789
          i32.mul
          local.tee 5
          i32.const 13
          i32.shr_u
          local.get 5
          i32.xor
          i32.const 1
          i32.and
          i32.store
          local.get 1
          i32.const 8
          i32.add
          local.set 1
          local.get 4
          local.get 2
          i32.const 2
          i32.add
          local.tee 2
          i32.ne
          br_if 0 (;@3;)
        end
        local.get 3
        i32.eqz
        br_if 1 (;@1;)
      end
      local.get 2
      i32.const 2
      i32.shl
      local.get 2
      i32.const -1640531527
      i32.add
      local.tee 2
      i32.const 16
      i32.shr_u
      local.get 2
      i32.xor
      i32.const -2048144789
      i32.mul
      local.tee 2
      i32.const 13
      i32.shr_u
      local.get 2
      i32.xor
      i32.const 1
      i32.and
      i32.store offset=65536
    end
    block  ;; label = @1
      local.get 0
      br_if 0 (;@1;)
      i32.const -2128831035
      return
    end
    i32.const 0
    local.get 0
    i32.const 2
    i32.shl
    local.tee 6
    i32.sub
    local.set 7
    i32.const -2128831035
    local.set 4
    i32.const 65536
    local.set 8
    i32.const 0
    local.set 2
    loop  ;; label = @1
      local.get 2
      local.get 0
      i32.lt_s
      local.get 2
      i32.const -1
      i32.gt_s
      i32.and
      local.set 3
      local.get 2
      i32.const 1
      i32.add
      local.tee 9
      local.get 0
      i32.lt_s
      local.get 9
      i32.const -1
      i32.gt_s
      i32.and
      local.set 10
      local.get 2
      i32.const -1
      i32.add
      local.tee 2
      local.get 0
      i32.lt_s
      local.get 2
      i32.const -1
      i32.gt_s
      i32.and
      local.set 11
      i32.const 0
      local.set 2
      local.get 8
      local.set 5
      loop  ;; label = @2
        i32.const 0
        local.set 1
        block  ;; label = @3
          local.get 11
          i32.eqz
          br_if 0 (;@3;)
          i32.const 0
          local.set 1
          block  ;; label = @4
            local.get 2
            i32.const 1
            i32.lt_s
            br_if 0 (;@4;)
            local.get 2
            local.get 0
            i32.gt_s
            br_if 0 (;@4;)
            local.get 5
            local.get 7
            i32.add
            i32.const -4
            i32.add
            i32.load
            local.set 1
          end
          block  ;; label = @4
            local.get 2
            i32.const 0
            i32.lt_s
            br_if 0 (;@4;)
            local.get 2
            local.get 0
            i32.ge_s
            br_if 0 (;@4;)
            local.get 5
            local.get 7
            i32.add
            i32.load
            local.get 1
            i32.or
            local.set 1
          end
          local.get 2
          i32.const -1
          i32.lt_s
          br_if 0 (;@3;)
          local.get 2
          i32.const 1
          i32.add
          local.get 0
          i32.ge_s
          br_if 0 (;@3;)
          local.get 5
          local.get 7
          i32.add
          i32.const 4
          i32.add
          i32.load
          local.get 1
          i32.or
          local.set 1
        end
        block  ;; label = @3
          local.get 3
          i32.eqz
          br_if 0 (;@3;)
          block  ;; label = @4
            local.get 2
            i32.const 1
            i32.lt_s
            br_if 0 (;@4;)
            local.get 2
            local.get 0
            i32.gt_s
            br_if 0 (;@4;)
            local.get 5
            i32.const -4
            i32.add
            i32.load
            local.get 1
            i32.or
            local.set 1
          end
          block  ;; label = @4
            local.get 2
            i32.const 0
            i32.lt_s
            br_if 0 (;@4;)
            local.get 2
            local.get 0
            i32.ge_s
            br_if 0 (;@4;)
            local.get 5
            i32.load
            local.get 1
            i32.or
            local.set 1
          end
          local.get 2
          i32.const -1
          i32.lt_s
          br_if 0 (;@3;)
          local.get 2
          i32.const 1
          i32.add
          local.get 0
          i32.ge_s
          br_if 0 (;@3;)
          local.get 5
          i32.const 4
          i32.add
          i32.load
          local.get 1
          i32.or
          local.set 1
        end
        block  ;; label = @3
          local.get 10
          i32.eqz
          br_if 0 (;@3;)
          block  ;; label = @4
            local.get 2
            i32.const 1
            i32.lt_s
            br_if 0 (;@4;)
            local.get 2
            local.get 0
            i32.gt_s
            br_if 0 (;@4;)
            local.get 5
            local.get 6
            i32.add
            i32.const -4
            i32.add
            i32.load
            local.get 1
            i32.or
            local.set 1
          end
          block  ;; label = @4
            local.get 2
            i32.const 0
            i32.lt_s
            br_if 0 (;@4;)
            local.get 2
            local.get 0
            i32.ge_s
            br_if 0 (;@4;)
            local.get 5
            local.get 6
            i32.add
            i32.load
            local.get 1
            i32.or
            local.set 1
          end
          local.get 2
          i32.const 1
          i32.add
          local.tee 12
          i32.const 0
          i32.lt_s
          br_if 0 (;@3;)
          local.get 12
          local.get 0
          i32.ge_s
          br_if 0 (;@3;)
          local.get 5
          local.get 6
          i32.add
          i32.const 4
          i32.add
          i32.load
          local.get 1
          i32.or
          local.set 1
        end
        local.get 5
        i32.const 4
        i32.add
        local.set 5
        local.get 1
        local.get 4
        i32.xor
        i32.const 16777619
        i32.mul
        local.set 4
        local.get 0
        local.get 2
        i32.const 1
        i32.add
        local.tee 2
        i32.ne
        br_if 0 (;@2;)
      end
      local.get 8
      local.get 6
      i32.add
      local.set 8
      local.get 9
      local.set 2
      local.get 9
      local.get 0
      i32.ne
      br_if 0 (;@1;)
    end
    local.get 4)
  (memory (;0;) 5 36)
  (global (;0;) (mut i32) (i32.const 65536))
  (export "memory" (memory 0))
  (export "benchmark" (func 0)))
