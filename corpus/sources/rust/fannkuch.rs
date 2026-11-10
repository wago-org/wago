// fannkuch-redux — the Computer Language Benchmarks Game permutation kernel,
// ported to a no_std wasm32 module. Generates every permutation of 1..=n in
// order and counts pancake-flips; branch- and array-churn heavy with no f64 and
// almost no memory traffic (the two working arrays live on the shadow stack).
// A stress test for the backend's integer/control codegen and local churn.
//
// Exported: run(n) returns the maximum flip count over all permutations of n
// elements (the canonical fannkuch(n) answer). n is clamped to [1, 12].
#![no_std]
#![no_main]
#[panic_handler]
fn ph(_: &core::panic::PanicInfo) -> ! { loop {} }

const MAX: usize = 12;

#[no_mangle]
pub extern "C" fn run(n_in: i32) -> i32 {
    let n = if n_in < 1 { 1 } else if n_in as usize > MAX { MAX } else { n_in as usize };
    let mut perm = [0i32; MAX];
    let mut perm1 = [0i32; MAX];
    let mut count = [0i32; MAX];
    for i in 0..n { perm1[i] = i as i32; }
    let mut max_flips = 0i32;
    let mut r = n;
    loop {
        while r != 1 { count[r - 1] = r as i32; r -= 1; }
        for i in 0..n { perm[i] = perm1[i]; }
        // Count flips: repeatedly reverse the first perm[0]+1 elements.
        let mut flips = 0i32;
        let mut k = perm[0];
        while k != 0 {
            let ku = k as usize;
            let mut i = 0usize;
            let mut j = ku;
            while i < j {
                let t = perm[i]; perm[i] = perm[j]; perm[j] = t;
                i += 1; j -= 1;
            }
            flips += 1;
            k = perm[0];
        }
        if flips > max_flips { max_flips = flips; }
        // Advance to the next permutation (single rotation scheme).
        loop {
            if r == n { return max_flips; }
            let perm0 = perm1[0];
            let mut i = 0usize;
            while i < r { perm1[i] = perm1[i + 1]; i += 1; }
            perm1[r] = perm0;
            count[r] -= 1;
            if count[r] > 0 { break; }
            r += 1;
        }
    }
}
