// nbody — the Computer Language Benchmarks Game N-body kernel, ported to a
// no_std wasm32 module. Integrates the Jovian planets of the solar system with
// a symplectic leapfrog step; f64-heavy (mul/add/sqrt/div), no linear-memory
// traffic beyond the fixed body table on the shadow stack. Each call re-seeds
// the same initial conditions so repeated bench invocations are identical.
//
// Exported: step(n) advances n leapfrog steps at dt=0.01 and returns the total
// energy scaled by 1e9 as an i32 (a DCE sink the host can print/compare).
#![no_std]
#![no_main]
#[panic_handler]
fn ph(_: &core::panic::PanicInfo) -> ! { loop {} }

const PI: f64 = 3.141592653589793;
const SOLAR_MASS: f64 = 4.0 * PI * PI;
const DAYS_PER_YEAR: f64 = 365.24;
const N: usize = 5;

#[derive(Clone, Copy)]
struct Body { x: f64, y: f64, z: f64, vx: f64, vy: f64, vz: f64, mass: f64 }

fn system() -> [Body; N] {
    let mut b = [
        // Sun
        Body { x: 0.0, y: 0.0, z: 0.0, vx: 0.0, vy: 0.0, vz: 0.0, mass: SOLAR_MASS },
        // Jupiter
        Body { x: 4.841431442464721, y: -1.1603200440274284, z: -0.10362204447112311,
               vx: 0.001660076642744037 * DAYS_PER_YEAR, vy: 0.007699011184197404 * DAYS_PER_YEAR,
               vz: -0.0000690460016972063 * DAYS_PER_YEAR, mass: 0.0009547919384243266 * SOLAR_MASS },
        // Saturn
        Body { x: 8.34336671824458, y: 4.124798564124305, z: -0.4035234171143214,
               vx: -0.002767425107268624 * DAYS_PER_YEAR, vy: 0.004998528012349172 * DAYS_PER_YEAR,
               vz: 0.0000230417297573763 * DAYS_PER_YEAR, mass: 0.0002858859806661308 * SOLAR_MASS },
        // Uranus
        Body { x: 12.894369562139131, y: -15.111151401698631, z: -0.22330757889265573,
               vx: 0.002964601375647616 * DAYS_PER_YEAR, vy: 0.0023784717395948095 * DAYS_PER_YEAR,
               vz: -0.0000296589568540238 * DAYS_PER_YEAR, mass: 0.00004366244043351563 * SOLAR_MASS },
        // Neptune
        Body { x: 15.379697114850917, y: -25.919314609987964, z: 0.17925877295037118,
               vx: 0.0026806777249038932 * DAYS_PER_YEAR, vy: 0.001628241700382423 * DAYS_PER_YEAR,
               vz: -0.00009515922545197159 * DAYS_PER_YEAR, mass: 0.00005151389020466116 * SOLAR_MASS },
    ];
    // Offset the momentum of the sun so the system's total momentum is zero.
    let (mut px, mut py, mut pz) = (0.0, 0.0, 0.0);
    for i in 0..N { px += b[i].vx * b[i].mass; py += b[i].vy * b[i].mass; pz += b[i].vz * b[i].mass; }
    b[0].vx = -px / SOLAR_MASS; b[0].vy = -py / SOLAR_MASS; b[0].vz = -pz / SOLAR_MASS;
    b
}

// Self-contained sqrt (stable core has no f64::sqrt): a bit-hack seed refined
// by Newton–Raphson to full double precision. Keeps the module import-free.
fn sqrt(x: f64) -> f64 {
    if x <= 0.0 { return 0.0; }
    let i = x.to_bits();
    let mut y = f64::from_bits((i >> 1) + (1023u64 << 51));
    y = 0.5 * (y + x / y); y = 0.5 * (y + x / y); y = 0.5 * (y + x / y);
    y = 0.5 * (y + x / y); y = 0.5 * (y + x / y);
    y
}

fn advance(b: &mut [Body; N], dt: f64) {
    for i in 0..N {
        for j in (i + 1)..N {
            let dx = b[i].x - b[j].x;
            let dy = b[i].y - b[j].y;
            let dz = b[i].z - b[j].z;
            let d2 = dx * dx + dy * dy + dz * dz;
            let mag = dt / (d2 * sqrt(d2));
            let (im, jm) = (b[i].mass, b[j].mass);
            b[i].vx -= dx * jm * mag; b[i].vy -= dy * jm * mag; b[i].vz -= dz * jm * mag;
            b[j].vx += dx * im * mag; b[j].vy += dy * im * mag; b[j].vz += dz * im * mag;
        }
    }
    for i in 0..N { b[i].x += dt * b[i].vx; b[i].y += dt * b[i].vy; b[i].z += dt * b[i].vz; }
}

fn energy(b: &[Body; N]) -> f64 {
    let mut e = 0.0;
    for i in 0..N {
        e += 0.5 * b[i].mass * (b[i].vx * b[i].vx + b[i].vy * b[i].vy + b[i].vz * b[i].vz);
        for j in (i + 1)..N {
            let dx = b[i].x - b[j].x;
            let dy = b[i].y - b[j].y;
            let dz = b[i].z - b[j].z;
            e -= b[i].mass * b[j].mass / sqrt(dx * dx + dy * dy + dz * dz);
        }
    }
    e
}

#[no_mangle]
pub extern "C" fn step(n: i32) -> i32 {
    let mut b = system();
    let mut i = 0;
    while i < n { advance(&mut b, 0.01); i += 1; }
    (energy(&b) * 1.0e9) as i32
}

// _start wrapper (startup-latency twin): run a representative workload once and
// sink the result so the whole nbody kernel stays live through DCE.
#[no_mangle]
pub extern "C" fn _start() {
    let mut s = 0i32;
    let mut k = 0i32;
    while k < 1 {
        s = s.wrapping_add(step(200000));
        k += 1;
    }
    core::hint::black_box(s);
}
