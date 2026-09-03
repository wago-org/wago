#include <WAVM/wavm-c/wavm-c.h>

#include <inttypes.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

static void fail(const char* message) {
  fprintf(stderr, "wavm-execution-worker: %s\n", message);
  exit(1);
}

static uint64_t now_ns(void) {
  struct timespec value;
  if (clock_gettime(CLOCK_MONOTONIC, &value) != 0) fail("clock_gettime failed");
  return (uint64_t)value.tv_sec * 1000000000ull + (uint64_t)value.tv_nsec;
}

static char* read_file(const char* path, size_t* size) {
  FILE* file = fopen(path, "rb");
  if (!file) fail("cannot open Wasm module");
  fseek(file, 0, SEEK_END);
  long length = ftell(file);
  fseek(file, 0, SEEK_SET);
  char* bytes = malloc((size_t)length);
  if (!bytes || fread(bytes, 1, (size_t)length, file) != (size_t)length) fail("cannot read Wasm module");
  fclose(file);
  *size = (size_t)length;
  return bytes;
}

static wasm_trap_t* default_host(const wasm_val_t args[], wasm_val_t results[]) {
  (void)args;
  memset(results, 0, sizeof(*results));
  return NULL;
}

typedef struct runtime_state {
  wasm_engine_t* engine;
  wasm_compartment_t* compartment;
  wasm_store_t* store;
  wasm_module_t* module;
  wasm_func_t** imported_funcs;
  const wasm_extern_t** imports;
  size_t num_imports;
} runtime_state;

static runtime_state make_state(wasm_engine_t* engine, wasm_module_t* module) {
  runtime_state s = {0};
  s.engine = engine;
  s.module = module;
  s.compartment = wasm_compartment_new(engine, "benchmark");
  s.store = wasm_store_new(s.compartment, "benchmark");
  s.num_imports = wasm_module_num_imports(module);
  s.imported_funcs = calloc(s.num_imports, sizeof(*s.imported_funcs));
  s.imports = calloc(s.num_imports, sizeof(*s.imports));
  for (size_t i = 0; i < s.num_imports; i++) {
    wasm_import_t item;
    wasm_module_import(module, i, &item);
    if (wasm_externtype_kind(item.type) != WASM_EXTERN_FUNC) fail("only function imports are supported");
    s.imported_funcs[i] = wasm_func_new(s.compartment,
      wasm_externtype_as_functype(item.type), default_host, "default import");
    s.imports[i] = wasm_func_as_extern(s.imported_funcs[i]);
  }
  return s;
}

static void destroy_state(runtime_state* s) {
  for (size_t i = 0; i < s->num_imports; i++) wasm_func_delete(s->imported_funcs[i]);
  free(s->imports);
  free(s->imported_funcs);
  wasm_store_delete(s->store);
  wasm_compartment_delete(s->compartment);
}

static wasm_instance_t* instantiate(runtime_state* s) {
  wasm_trap_t* trap = NULL;
  wasm_instance_t* instance = wasm_instance_new(s->store, s->module, s->imports, &trap, "benchmark");
  if (!instance) fail("instantiation failed");
  return instance;
}

static wasm_func_t* find_function(wasm_module_t* module, wasm_instance_t* instance, const char* name) {
  size_t count = wasm_module_num_exports(module);
  for (size_t i = 0; i < count; i++) {
    wasm_export_t item;
    wasm_module_export(module, i, &item);
    if (strlen(name) == item.num_name_bytes && !memcmp(name, item.name, item.num_name_bytes)) {
      return wasm_extern_as_func(wasm_instance_export(instance, i));
    }
  }
  return NULL;
}

static uint64_t run_instantiations(runtime_state* s, uint64_t iterations) {
  uint64_t started = now_ns();
  for (uint64_t i = 0; i < iterations; i++) {
    wasm_instance_t* instance = instantiate(s);
    wasm_instance_delete(instance);
  }
  return now_ns() - started;
}

static uint64_t run_calls(wasm_store_t* store, wasm_func_t* function, wasm_val_t* args,
                          wasm_val_t* results, uint64_t iterations) {
  uint64_t started = now_ns();
  for (uint64_t i = 0; i < iterations; i++) {
    wasm_trap_t* trap = wasm_func_call(store, function, args, results);
    if (trap) fail("export trapped");
  }
  return now_ns() - started;
}

static uint64_t calibrate(uint64_t (*run)(void*, uint64_t), void* context,
                          uint64_t target_ns, uint64_t limit) {
  uint64_t iterations = 1;
  for (;;) {
    uint64_t elapsed = run(context, iterations);
    if (elapsed >= target_ns / 10 || iterations >= limit) {
      long double scaled = (long double)iterations * target_ns / (elapsed ? elapsed : 1);
      return scaled < 1 ? 1 : (uint64_t)scaled;
    }
    iterations *= 10;
  }
}

typedef struct call_state { wasm_store_t* store; wasm_func_t* function; wasm_val_t* args; wasm_val_t* results; } call_state;
static uint64_t call_adapter(void* p, uint64_t n) {
  call_state* s = p;
  return run_calls(s->store, s->function, s->args, s->results, n);
}
static uint64_t instantiate_adapter(void* p, uint64_t n) { return run_instantiations(p, n); }

int main(int argc, char** argv) {
  const char *module_path = NULL, *init_name = NULL, *export_name = NULL, *args_text = "", *out_path = NULL;
  int round = 0;
  bool measure_instantiate = false;
  uint64_t target_ns = 100000000;
  for (int i = 1; i < argc; i++) {
    if (!strcmp(argv[i], "-module") && ++i < argc) module_path = argv[i];
    else if (!strcmp(argv[i], "-init") && ++i < argc) init_name = argv[i];
    else if (!strcmp(argv[i], "-export") && ++i < argc) export_name = argv[i];
    else if (!strcmp(argv[i], "-args") && ++i < argc) args_text = argv[i];
    else if (!strcmp(argv[i], "-round") && ++i < argc) round = atoi(argv[i]);
    else if (!strcmp(argv[i], "-benchtime-ns") && ++i < argc) target_ns = strtoull(argv[i], NULL, 10);
    else if (!strcmp(argv[i], "-out") && ++i < argc) out_path = argv[i];
    else if (!strcmp(argv[i], "-measure-instantiate")) measure_instantiate = true;
    else fail("invalid arguments");
  }
  if (!module_path || !export_name || !out_path || !target_ns) fail("required argument missing");

  size_t wasm_size = 0;
  char* wasm = read_file(module_path, &wasm_size);
  wasm_engine_t* engine = wasm_engine_new();
  wasm_module_t* module = wasm_module_new(engine, wasm, wasm_size);
  free(wasm);
  if (!module) fail("compilation failed");
  runtime_state state = make_state(engine, module);
  FILE* output = fopen(out_path, "a");
  if (!output) fail("cannot open output");

  if (measure_instantiate) {
    run_instantiations(&state, 1);
    uint64_t iterations = calibrate(instantiate_adapter, &state, target_ns, 1ull << 30);
    uint64_t elapsed = run_instantiations(&state, iterations);
    fprintf(output, "{\"engine\":\"wavm\",\"stage\":\"instantiate\",\"module\":\"%s\",\"round\":%d,\"iterations\":%" PRIu64 ",\"elapsed_ns\":%" PRIu64 ",\"ns_per_op\":%.9Lf}\n",
      module_path, round, iterations, elapsed, (long double)elapsed / iterations);
  }

  wasm_instance_t* instance = instantiate(&state);
  if (init_name && init_name[0]) {
    wasm_func_t* init = find_function(module, instance, init_name);
    if (!init || wasm_func_call(state.store, init, NULL, NULL)) fail("initialization failed");
  }
  wasm_func_t* function = find_function(module, instance, export_name);
  if (!function) fail("export is not a function");
  wasm_functype_t* type = wasm_func_type(function);
  size_t nargs = wasm_functype_num_params(type), nresults = wasm_functype_num_results(type);
  wasm_val_t* args = calloc(nargs, sizeof(*args));
  wasm_val_t* results = calloc(nresults, sizeof(*results));
  char* copy = strdup(args_text);
  char* save = NULL;
  size_t index = 0;
  for (char* part = strtok_r(copy, ",", &save); part; part = strtok_r(NULL, ",", &save)) {
    if (index >= nargs || wasm_valtype_kind(wasm_functype_param(type, index)) != WASM_I32) fail("expected i32 arguments");
    args[index++].i32 = (int32_t)strtol(part, NULL, 10);
  }
  if (index != nargs) fail("argument count mismatch");
  call_state calls = {state.store, function, args, results};
  run_calls(state.store, function, args, results, 1);
  uint64_t iterations = calibrate(call_adapter, &calls, target_ns, 1ull << 40);
  uint64_t elapsed = run_calls(state.store, function, args, results, iterations);
  fprintf(output, "{\"engine\":\"wavm\",\"stage\":\"exec\",\"module\":\"%s\",\"export\":\"%s\",\"round\":%d,\"iterations\":%" PRIu64 ",\"elapsed_ns\":%" PRIu64 ",\"ns_per_op\":%.9Lf}\n",
    module_path, export_name, round, iterations, elapsed, (long double)elapsed / iterations);

  fclose(output);
  free(copy); free(args); free(results);
  wasm_functype_delete(type);
  wasm_instance_delete(instance);
  destroy_state(&state);
  wasm_module_delete(module);
  wasm_engine_delete(engine);
  return 0;
}
