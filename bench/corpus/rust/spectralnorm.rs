// spectralnorm — the Computer Language Benchmarks Game kernel, ported to a
// no_std wasm32 module. Approximates the largest singular value of an infinite
// matrix A (a_ij = 1/((i+j)(i+j+1)/2 + i + 1)) via a few power-iteration passes
// of AᵀA. f64-heavy with an integer-division-per-element inner loop over fixed
// stack arrays — exercises div throughput alongside mul/add/sqrt.
//
// Exported: run(n) returns the spectral-norm estimate scaled by 1e9 as an i32.
// n is clamped to [1, 2048] (the working vectors are fixed 2048-wide arrays).
#![no_std]
#![no_main]
#[panic_handler]
fn ph(_: &core::panic::PanicInfo) -> ! { loop {} }

const MAX: usize = 2048;

fn sqrt(x: f64) -> f64 {
    if x <= 0.0 { return 0.0; }
    let i = x.to_bits();
    let mut y = f64::from_bits((i >> 1) + (1023u64 << 51));
    y = 0.5 * (y + x / y); y = 0.5 * (y + x / y); y = 0.5 * (y + x / y);
    y = 0.5 * (y + x / y); y = 0.5 * (y + x / y);
    y
}

#[inline]
fn a(i: usize, j: usize) -> f64 {
    let s = i + j;
    (1.0) / ((s * (s + 1) / 2 + i + 1) as f64)
}

fn mul_av(n: usize, v: &[f64; MAX], out: &mut [f64; MAX]) {
    for i in 0..n {
        let mut sum = 0.0;
        for j in 0..n { sum += a(i, j) * v[j]; }
        out[i] = sum;
    }
}

fn mul_atv(n: usize, v: &[f64; MAX], out: &mut [f64; MAX]) {
    for i in 0..n {
        let mut sum = 0.0;
        for j in 0..n { sum += a(j, i) * v[j]; }
        out[i] = sum;
    }
}

fn mul_atav(n: usize, v: &[f64; MAX], out: &mut [f64; MAX], tmp: &mut [f64; MAX]) {
    mul_av(n, v, tmp);
    mul_atv(n, tmp, out);
}

#[no_mangle]
pub extern "C" fn run(n_in: i32) -> i32 {
    let n = if n_in < 1 { 1 } else if n_in as usize > MAX { MAX } else { n_in as usize };
    let mut u = [1.0f64; MAX];
    let mut v = [0.0f64; MAX];
    let mut tmp = [0.0f64; MAX];
    for _ in 0..10 {
        mul_atav(n, &u, &mut v, &mut tmp);
        mul_atav(n, &v, &mut u, &mut tmp);
    }
    let mut vbv = 0.0f64;
    let mut vv = 0.0f64;
    for i in 0..n { vbv += u[i] * v[i]; vv += v[i] * v[i]; }
    (sqrt(vbv / vv) * 1.0e9) as i32
}
