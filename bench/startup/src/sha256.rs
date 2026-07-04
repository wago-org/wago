// sha256 — a full, standards-correct SHA-256 implementation as a no_std wasm32
// kernel. Hashes an n-KiB deterministic buffer by processing 64-byte blocks
// through the 64-round compression function. Rotate/shift/xor/add-heavy 32-bit
// integer work with a fixed round-constant table — a real cryptographic
// primitive rather than a synthetic loop, and a good exercise of i32 rotates
// and the big fixed constant pool.
//
// Exported: hashN(n) hashes an (n * 1024)-byte buffer and returns the first
// 32 bits of the digest as an i32. n is clamped to [1, 64].
#![no_std]
#![no_main]
#[panic_handler]
fn ph(_: &core::panic::PanicInfo) -> ! { loop {} }

const KIB: usize = 1024;
const MAXK: usize = 64;

const K: [u32; 64] = [
    0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
    0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
    0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
    0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
    0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
    0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
    0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
    0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
];

fn block(h: &mut [u32; 8], p: &[u8]) {
    let mut w = [0u32; 64];
    for i in 0..16 {
        w[i] = ((p[i * 4] as u32) << 24)
            | ((p[i * 4 + 1] as u32) << 16)
            | ((p[i * 4 + 2] as u32) << 8)
            | (p[i * 4 + 3] as u32);
    }
    for i in 16..64 {
        let s0 = w[i - 15].rotate_right(7) ^ w[i - 15].rotate_right(18) ^ (w[i - 15] >> 3);
        let s1 = w[i - 2].rotate_right(17) ^ w[i - 2].rotate_right(19) ^ (w[i - 2] >> 10);
        w[i] = w[i - 16]
            .wrapping_add(s0)
            .wrapping_add(w[i - 7])
            .wrapping_add(s1);
    }
    let (mut a, mut b, mut c, mut d, mut e, mut f, mut g, mut hh) =
        (h[0], h[1], h[2], h[3], h[4], h[5], h[6], h[7]);
    for i in 0..64 {
        let s1 = e.rotate_right(6) ^ e.rotate_right(11) ^ e.rotate_right(25);
        let ch = (e & f) ^ ((!e) & g);
        let t1 = hh
            .wrapping_add(s1)
            .wrapping_add(ch)
            .wrapping_add(K[i])
            .wrapping_add(w[i]);
        let s0 = a.rotate_right(2) ^ a.rotate_right(13) ^ a.rotate_right(22);
        let maj = (a & b) ^ (a & c) ^ (b & c);
        let t2 = s0.wrapping_add(maj);
        hh = g; g = f; f = e; e = d.wrapping_add(t1);
        d = c; c = b; b = a; a = t1.wrapping_add(t2);
    }
    h[0] = h[0].wrapping_add(a); h[1] = h[1].wrapping_add(b);
    h[2] = h[2].wrapping_add(c); h[3] = h[3].wrapping_add(d);
    h[4] = h[4].wrapping_add(e); h[5] = h[5].wrapping_add(f);
    h[6] = h[6].wrapping_add(g); h[7] = h[7].wrapping_add(hh);
}

#[no_mangle]
pub extern "C" fn hashN(n_in: i32) -> i32 {
    let k = if n_in < 1 { 1 } else if n_in as usize > MAXK { MAXK } else { n_in as usize };
    let len = k * KIB;
    let mut buf = [0u8; MAXK * KIB + 128];
    let mut s: u32 = 0xDEAD_BEEF;
    for i in 0..len {
        s = s.wrapping_mul(1_664_525).wrapping_add(1_013_904_223);
        buf[i] = (s >> 24) as u8;
    }
    // Pad: 0x80, zeros, then the 64-bit big-endian bit length.
    let bitlen = (len as u64) * 8;
    buf[len] = 0x80;
    let mut total = len + 1;
    while total % 64 != 56 { buf[total] = 0; total += 1; }
    for i in 0..8 { buf[total + i] = (bitlen >> (56 - i * 8)) as u8; }
    total += 8;

    let mut h: [u32; 8] = [
        0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a,
        0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19,
    ];
    let mut off = 0;
    while off < total {
        block(&mut h, &buf[off..off + 64]);
        off += 64;
    }
    h[0] as i32
}

#[no_mangle]
pub extern "C" fn _start() { let mut s=0i32; let mut k=0i32; while k<1 { s=s.wrapping_add(hashN(64)); k+=1; } core::hint::black_box(s); }
