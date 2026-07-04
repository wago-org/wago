// quicksort — in-place recursive quicksort over a synthetic i32 array, a no_std
// wasm32 kernel. Fills a fixed array with an LCG-shuffled stream, sorts it, and
// returns a position-weighted checksum (which also proves the sort ran). Mixes
// recursion (internal call/return), branchy partitioning, and swap-heavy linear
// memory traffic — the integer analogue of memory_tree's call+memory churn.
//
// Exported: sortN(n) sorts n elements and returns a checksum as an i32.
// n is clamped to [1, 16384] (the working array is a fixed 16384-wide array).
#![no_std]
#![no_main]
#[panic_handler]
fn ph(_: &core::panic::PanicInfo) -> ! { loop {} }

const MAX: usize = 16384;

fn quicksort(a: &mut [i32], lo: isize, hi: isize) {
    if lo >= hi { return; }
    let pivot = a[((lo + hi) / 2) as usize];
    let mut i = lo;
    let mut j = hi;
    while i <= j {
        while a[i as usize] < pivot { i += 1; }
        while a[j as usize] > pivot { j -= 1; }
        if i <= j {
            a.swap(i as usize, j as usize);
            i += 1;
            j -= 1;
        }
    }
    quicksort(a, lo, j);
    quicksort(a, i, hi);
}

#[no_mangle]
pub extern "C" fn sortN(n_in: i32) -> i32 {
    let n = if n_in < 1 { 1 } else if n_in as usize > MAX { MAX } else { n_in as usize };
    let mut a = [0i32; MAX];
    let mut s: u32 = 0x9E37_79B9;
    for i in 0..n {
        s = s.wrapping_mul(1_664_525).wrapping_add(1_013_904_223);
        a[i] = (s >> 8) as i32;
    }
    quicksort(&mut a[..n], 0, n as isize - 1);
    let mut sum: i32 = 0;
    for i in 0..n {
        sum = sum.wrapping_add(a[i].wrapping_mul(i as i32 + 1));
    }
    sum
}
