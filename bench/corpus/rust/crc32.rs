// crc32 — table-driven CRC-32 (IEEE polynomial) over a synthetic buffer, a
// no_std wasm32 kernel. Builds the 256-entry lookup table on each call, fills an
// n-KiB buffer in linear memory with a deterministic byte pattern, and folds it
// through the table. Pure integer: byte loads, xor, shift, table-indexed loads —
// the counterpart to the f64 kernels for exercising the integer/memory path.
//
// Exported: hashN(n) returns the CRC-32 of an (n * 1024)-byte buffer as an i32.
// n is clamped to [1, 64] (the buffer is a fixed 64 KiB array).
#![no_std]
#![no_main]
#[panic_handler]
fn ph(_: &core::panic::PanicInfo) -> ! { loop {} }

const KIB: usize = 1024;
const MAXK: usize = 64;

#[no_mangle]
pub extern "C" fn hashN(n_in: i32) -> i32 {
    let k = if n_in < 1 { 1 } else if n_in as usize > MAXK { MAXK } else { n_in as usize };
    let len = k * KIB;
    let mut buf = [0u8; MAXK * KIB];
    // Deterministic content: a simple LCG stream so every run hashes the same.
    let mut s: u32 = 0x1234_5678;
    for i in 0..len {
        s = s.wrapping_mul(1_664_525).wrapping_add(1_013_904_223);
        buf[i] = (s >> 24) as u8;
    }
    let mut table = [0u32; 256];
    for i in 0..256u32 {
        let mut c = i;
        for _ in 0..8 {
            c = if c & 1 != 0 { 0xEDB8_8320 ^ (c >> 1) } else { c >> 1 };
        }
        table[i as usize] = c;
    }
    let mut crc: u32 = 0xFFFF_FFFF;
    for i in 0..len {
        let idx = ((crc ^ buf[i] as u32) & 0xFF) as usize;
        crc = table[idx] ^ (crc >> 8);
    }
    (crc ^ 0xFFFF_FFFF) as i32
}
