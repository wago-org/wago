// matmul — dense square f64 matrix multiply, a no_std wasm32 kernel. Fills two
// n×n matrices with a deterministic pattern, computes C = A·B with the classic
// triple loop, and returns a checksum of C. Combines heavy fp multiply-add with
// strided linear-memory loads over three fixed matrices — the shape that most
// stresses register allocation across a hot inner loop touching memory.
//
// Exported: run(n) returns the truncated trace-plus-checksum of C as an i32.
// n is clamped to [1, 96] (matrices are fixed 96×96 = 9216 f64 each).
#![no_std]
#![no_main]
#[panic_handler]
fn ph(_: &core::panic::PanicInfo) -> ! { loop {} }

const MAX: usize = 96;

#[no_mangle]
pub extern "C" fn run(n_in: i32) -> i32 {
    let n = if n_in < 1 { 1 } else if n_in as usize > MAX { MAX } else { n_in as usize };
    let mut a = [0.0f64; MAX * MAX];
    let mut b = [0.0f64; MAX * MAX];
    let mut c = [0.0f64; MAX * MAX];
    // Deterministic fill: cheap rationals so results are exact across engines.
    for i in 0..n {
        for j in 0..n {
            a[i * n + j] = ((i + 1) as f64) / ((j + 1) as f64);
            b[i * n + j] = ((i * 2 + 1) as f64) / ((j + 3) as f64);
        }
    }
    for i in 0..n {
        for k in 0..n {
            let aik = a[i * n + k];
            for j in 0..n {
                c[i * n + j] += aik * b[k * n + j];
            }
        }
    }
    let mut sum = 0.0f64;
    for i in 0..n { sum += c[i * n + i]; }
    (sum * 1000.0) as i32
}
