.section .text.wago_pico2_native_invoke, "ax", @progbits
.global wago_pico2_native_invoke
.type wago_pico2_native_invoke, @function
wago_pico2_native_invoke:
    addi sp, sp, -16
    sw ra, 12(sp)
    sw s0, 8(sp)
    sw s1, 4(sp)
    mv s0, sp
    mv s1, a0
    mv sp, a2
    andi sp, sp, -16
    mv a0, a1
    jalr ra, s1, 0
    mv sp, s0
    lw s1, 4(sp)
    lw s0, 8(sp)
    lw ra, 12(sp)
    addi sp, sp, 16
    ret
.size wago_pico2_native_invoke, .-wago_pico2_native_invoke

.section .text.wago_pico2_publish, "ax", @progbits
.global wago_pico2_publish
.type wago_pico2_publish, @function
wago_pico2_publish:
    fence rw, rw
    fence.i
    ret
.size wago_pico2_publish, .-wago_pico2_publish

.macro HELPER_ADDRESS wrapper, target
.section .text.\wrapper, "ax", @progbits
.global \wrapper
.type \wrapper, @function
\wrapper:
    la a0, \target
    ret
.size \wrapper, .-\wrapper
.endm

HELPER_ADDRESS wago_pico2_helper_f64, wago_embedded32_f64
HELPER_ADDRESS wago_pico2_helper_simd, wago_embedded32_simd_abi
HELPER_ADDRESS wago_pico2_helper_i64, wago_embedded32_i64
HELPER_ADDRESS wago_pico2_helper_f32, wago_embedded32_f32
