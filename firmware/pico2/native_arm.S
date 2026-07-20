.syntax unified
.thumb

.section .text.wago_pico2_native_invoke, "ax", %progbits
.global wago_pico2_native_invoke
.type wago_pico2_native_invoke, %function
.thumb_func
wago_pico2_native_invoke:
	push {r4, r5, r6, lr}
	mov r4, r0
	mov r6, sp
	mov sp, r2
    mov r0, r1
    blx r4
	mov sp, r6
	pop {r4, r5, r6, pc}
.size wago_pico2_native_invoke, .-wago_pico2_native_invoke

.section .text.wago_pico2_publish, "ax", %progbits
.global wago_pico2_publish
.type wago_pico2_publish, %function
.thumb_func
wago_pico2_publish:
    dsb sy
    isb sy
    bx lr
.size wago_pico2_publish, .-wago_pico2_publish

.macro HELPER_ADDRESS wrapper, target
.section .text.\wrapper, "ax", %progbits
.global \wrapper
.type \wrapper, %function
.thumb_func
\wrapper:
    ldr r0, =\target
    orr r0, r0, #1
    bx lr
.size \wrapper, .-\wrapper
.endm

HELPER_ADDRESS wago_pico2_helper_f64, wago_embedded32_f64
HELPER_ADDRESS wago_pico2_helper_simd, wago_embedded32_simd_abi
HELPER_ADDRESS wago_pico2_helper_i64, wago_embedded32_i64
HELPER_ADDRESS wago_pico2_helper_f32, wago_embedded32_f32
