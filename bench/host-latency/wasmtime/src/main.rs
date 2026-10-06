use std::{
    hint::black_box,
    sync::{
        Arc,
        atomic::{AtomicU64, Ordering},
    },
    time::Instant,
};
use wasmtime::{Caller, Engine, Linker, Module, Store};

fn main() -> wasmtime::Result<()> {
    let mut args: Vec<_> = std::env::args().skip(1).collect();
    if matches!(
        args.first().map(String::as_str),
        Some("--entry" | "--entry-host")
    ) {
        return entry(args[0] == "--entry-host");
    }
    let multi = args.first().map(String::as_str) == Some("--multi");
    let with_atomic = multi || args.first().map(String::as_str) == Some("--yield-atomic");
    let yielding = with_atomic || args.first().map(String::as_str) == Some("--yield");
    if yielding {
        args.remove(0);
    }
    let counts = if args.is_empty() {
        vec![1, 1024, 65536]
    } else {
        args.iter()
            .map(|s| s.parse::<i32>())
            .collect::<Result<Vec<_>, _>>()?
    };
    assert!(counts.iter().all(|n| *n > 0), "loop count must be positive");
    let fixture = if multi {
        "multi"
    } else if yielding {
        "yield"
    } else {
        "loop"
    };
    let bytes = wat::parse_file(format!("{fixture}.wat"))?;
    std::fs::write(format!("{fixture}.wasm"), &bytes)?;
    let engine = Engine::default();
    let module = Module::new(&engine, bytes)?;
    for caller in [false, true] {
        let mut linker = Linker::new(&engine);
        let counter = Arc::new(AtomicU64::new(0));
        let counter2 = Arc::new(AtomicU64::new(0));
        let callback_counter = counter.clone();
        if with_atomic {
            if caller {
                linker.func_wrap("env", "step", move |_: Caller<'_, ()>, v: i32| {
                    callback_counter.fetch_add(1, Ordering::SeqCst);
                    v + 1
                })?;
            } else {
                linker.func_wrap("env", "step", move |v: i32| {
                    callback_counter.fetch_add(1, Ordering::SeqCst);
                    v + 1
                })?;
            }
        } else if caller {
            linker.func_wrap("env", "step", |_: Caller<'_, ()>, v: i32| v + 1)?;
        } else {
            linker.func_wrap("env", "step", |v: i32| v + 1)?;
        }
        if multi {
            let callback_counter = counter2.clone();
            if caller {
                linker.func_wrap("env", "step2", move |_: Caller<'_, ()>, v: i32| {
                    callback_counter.fetch_add(1, Ordering::SeqCst);
                    v + 1
                })?;
            } else {
                linker.func_wrap("env", "step2", move |v: i32| {
                    callback_counter.fetch_add(1, Ordering::SeqCst);
                    v + 1
                })?;
            }
        }
        let mut store = Store::new(&engine, ());
        let instance = linker.instantiate(&mut store, &module)?;
        let run = instance.get_typed_func::<(i32, i32), i32>(&mut store, "run")?;
        for &n in &counts {
            let repeats = (4_194_304 / n).max(64);
            let hosts: &[i32] = if yielding { &[1] } else { &[0, 1] };
            for &host in hosts {
                for _ in 0..16 {
                    assert_eq!(run.call(&mut store, (n, host))?, n);
                }
                for sample in 0..5 {
                    counter.store(0, Ordering::SeqCst);
                    counter2.store(0, Ordering::SeqCst);
                    let start = Instant::now();
                    for _ in 0..repeats {
                        assert_eq!(black_box(run.call(&mut store, (n, host))?), n);
                    }
                    let ns = start.elapsed().as_nanos() as f64 / (repeats as f64 * n as f64);
                    if multi {
                        assert_eq!(
                            counter.load(Ordering::SeqCst),
                            repeats as u64 * (n / 2 + n % 2) as u64
                        );
                        assert_eq!(
                            counter2.load(Ordering::SeqCst),
                            repeats as u64 * (n / 2) as u64
                        );
                    } else if with_atomic {
                        assert_eq!(counter.load(Ordering::SeqCst), repeats as u64 * n as u64);
                    }
                    println!(
                        "wasmtime,{}{},{n},{host},{sample},{ns:.4}",
                        match (caller, yielding) {
                            (false, false) => "typed",
                            (true, false) => "caller",
                            (false, true) => "typed-yield",
                            (true, true) => "caller-yield",
                        },
                        if multi {
                            "-multi-atomic"
                        } else if with_atomic {
                            "-atomic"
                        } else {
                            ""
                        }
                    );
                }
            }
        }
    }
    Ok(())
}

fn entry(with_host: bool) -> wasmtime::Result<()> {
    let name = if with_host { "entry-host" } else { "entry" };
    let bytes = wat::parse_file(format!("{name}.wat"))?;
    std::fs::write(format!("{name}.wasm"), &bytes)?;
    let engine = Engine::default();
    let module = Module::new(&engine, bytes)?;
    let mut store = Store::new(&engine, ());
    let instance = if with_host {
        let mut linker = Linker::new(&engine);
        linker.func_wrap("env", "step", |v: i32| -> i32 { v.wrapping_add(1) })?;
        linker.instantiate(&mut store, &module)?
    } else {
        wasmtime::Instance::new(&mut store, &module, &[])?
    };
    let add = instance.get_typed_func::<i32, i32>(&mut store, "add")?;
    for _ in 0..1024 {
        assert_eq!(add.call(&mut store, 41)?, 42);
    }
    for sample in 0..10 {
        let start = Instant::now();
        for i in 0..4_194_304 {
            let input = black_box(i & 1023);
            assert_eq!(add.call(&mut store, input)?, input + 1);
        }
        let ns = start.elapsed().as_nanos() as f64 / 4_194_304.0;
        println!("wasmtime,{name}-typed,1,1,{sample},{ns:.4}");
    }
    Ok(())
}
