// rhai: run a real embedded scripting-language program.
use rhai::Engine;
fn main() {
    let engine = Engine::new();
    let r: i64 = engine.eval(r#"
        let sum = 0;
        for i in 0..20000 { sum += (i * 3) % 1000007; if i % 2 == 0 { sum -= 1; } }
        sum
    "#).unwrap();
    println!("rhai:{}", r);
}
