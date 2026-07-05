// wasm3 end-to-end benchmark harness for wago's engine comparison.
//
// Mirrors the wago/wazero "Run" benchmark (bench/wasi_run_test.go): a real
// Rust/WASI program does its whole workload in _start, so one end-to-end "run"
// is parse + load + link-WASI + execute. This times exactly that, in-process
// (excluding this harness's own process startup, like WARP's vb_bench), and
// prints the best of N runs so benchpub can record it as Wasm3Run/<prog>.
//
// wasm3 is an interpreter, so this is the fair place to compare it against the
// JIT engines: total time to actually run a program, where its zero native-
// codegen startup partly offsets slower execution.
//
// Usage: wasm3_bench <file.wasm> <progname> [iters]   (default iters = 5)
// Output (stderr): "wasm3_run_ns: <ns>"   or   "wasm3_run_err: <msg>"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <unistd.h>
#include <fcntl.h>

#include "wasm3.h"
#include "m3_env.h"
#include "m3_api_wasi.h"

// The AS/Rust workloads need a roomy interpreter stack; the wasm3 CLI defaults
// to 64 KiB but the heavier corpus programs (rhai, regex) recurse deeper.
#define STACK_BYTES (512 * 1024)

static long long now_ns(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (long long)ts.tv_sec * 1000000000LL + (long long)ts.tv_nsec;
}

static unsigned char *load_file(const char *path, size_t *out_len) {
    FILE *f = fopen(path, "rb");
    if (!f) return NULL;
    fseek(f, 0, SEEK_END);
    long n = ftell(f);
    fseek(f, 0, SEEK_SET);
    if (n < 0) { fclose(f); return NULL; }
    unsigned char *buf = (unsigned char *)malloc((size_t)n);
    if (!buf) { fclose(f); return NULL; }
    if (fread(buf, 1, (size_t)n, f) != (size_t)n) { free(buf); fclose(f); return NULL; }
    fclose(f);
    *out_len = (size_t)n;
    return buf;
}

// One end-to-end run. Returns elapsed ns, or -1 on error (msg filled).
static long long run_once(const unsigned char *wasm, size_t wlen, char *prog, char **err) {
    long long t0 = now_ns();
    IM3Environment env = m3_NewEnvironment();
    IM3Runtime rt = m3_NewRuntime(env, STACK_BYTES, NULL);
    IM3Module mod = NULL;
    M3Result r = m3_ParseModule(env, &mod, wasm, (uint32_t)wlen);
    if (r) { *err = (char *)r; goto fail_env; }
    r = m3_LoadModule(rt, mod); // ownership of mod moves to rt on success
    if (r) { *err = (char *)r; m3_FreeModule(mod); goto fail_env; }
    r = m3_LinkWASI(mod);
    if (r) { *err = (char *)r; goto fail_rt; }

    m3_wasi_context_t *wc = m3_GetWasiContext();
    const char *wasi_argv[1] = { prog };
    wc->argc = 1;
    wc->argv = wasi_argv;
    wc->exit_code = 0;

    IM3Function fn = NULL;
    r = m3_FindFunction(&fn, rt, "_start");
    if (r) { *err = (char *)r; goto fail_rt; }
    r = m3_CallV(fn);
    // A WASI program ending via proc_exit surfaces as the trap-exit sentinel;
    // that's a normal completion, not an error.
    if (r && r != m3Err_trapExit) { *err = (char *)r; goto fail_rt; }

    m3_FreeRuntime(rt);
    m3_FreeEnvironment(env);
    return now_ns() - t0;

fail_rt:
    m3_FreeRuntime(rt);
fail_env:
    m3_FreeEnvironment(env);
    return -1;
}

int main(int argc, char **argv) {
    if (argc < 3) {
        fprintf(stderr, "usage: %s <file.wasm> <progname> [iters]\n", argv[0]);
        return 2;
    }
    const char *path = argv[1];
    char *prog = argv[2];
    int iters = argc > 3 ? atoi(argv[3]) : 5;
    if (iters < 1) iters = 1;

    size_t wlen = 0;
    unsigned char *wasm = load_file(path, &wlen);
    if (!wasm) { fprintf(stderr, "wasm3_run_err: cannot read %s\n", path); return 2; }

    // The workload writes its result to stdout via WASI fd_write; send that to
    // /dev/null so only the harness's own measurement reaches the caller. stderr
    // (where we report) stays intact.
    fflush(stdout);
    int saved = dup(STDOUT_FILENO);
    int devnull = open("/dev/null", O_WRONLY);
    if (devnull >= 0) dup2(devnull, STDOUT_FILENO);

    long long *samples = (long long *)malloc(sizeof(long long) * (size_t)iters);
    int n = 0;
    char *err = NULL;
    for (int i = 0; i < iters; i++) {
        char *e = NULL;
        long long dt = run_once(wasm, wlen, prog, &e);
        if (dt < 0) { err = e; break; }
        samples[n++] = dt;
    }

    if (devnull >= 0) { dup2(saved, STDOUT_FILENO); close(devnull); }
    if (saved >= 0) close(saved);
    free(wasm);

    if (n == 0) {
        fprintf(stderr, "wasm3_run_err: %s\n", err ? err : "unknown");
        free(samples);
        return 1;
    }
    // Report the median (matches the mean-of-many the Go Run benches report far
    // better than a min would, and is robust to the odd slow outlier).
    for (int i = 1; i < n; i++) { // insertion sort — n is tiny
        long long v = samples[i];
        int j = i - 1;
        while (j >= 0 && samples[j] > v) { samples[j + 1] = samples[j]; j--; }
        samples[j + 1] = v;
    }
    long long med = samples[n / 2];
    if (n % 2 == 0) med = (samples[n / 2 - 1] + samples[n / 2]) / 2;
    free(samples);
    fprintf(stderr, "wasm3_run_ns: %lld\n", med);
    return 0;
}
