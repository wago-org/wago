#include <wasmtime.h>

#include <errno.h>
#include <inttypes.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

static void fail(const char *message) {
  fprintf(stderr, "wasmtime-execution-worker: %s\n", message);
  exit(1);
}

static void check_error(wasmtime_error_t *error, wasm_trap_t *trap) {
  wasm_name_t message;
  if (error != NULL) {
    wasmtime_error_message(error, &message);
    fprintf(stderr, "wasmtime-execution-worker: %.*s\n", (int)message.size,
            message.data);
    wasm_name_delete(&message);
    wasmtime_error_delete(error);
    exit(1);
  }
  if (trap != NULL) {
    wasm_trap_message(trap, &message);
    fprintf(stderr, "wasmtime-execution-worker: trap: %.*s\n", (int)message.size,
            message.data);
    wasm_name_delete(&message);
    wasm_trap_delete(trap);
    exit(1);
  }
}

static uint64_t now_ns(void) {
  struct timespec value;
  if (clock_gettime(CLOCK_MONOTONIC_RAW, &value) != 0)
    fail("clock_gettime failed");
  return (uint64_t)value.tv_sec * 1000000000ull + (uint64_t)value.tv_nsec;
}

static uint8_t *read_file(const char *path, size_t *size) {
  FILE *file = fopen(path, "rb");
  if (file == NULL)
    fail("cannot open Wasm module");
  if (fseek(file, 0, SEEK_END) != 0)
    fail("cannot seek Wasm module");
  long length = ftell(file);
  if (length < 0 || fseek(file, 0, SEEK_SET) != 0)
    fail("cannot size Wasm module");
  uint8_t *bytes = malloc((size_t)length);
  if (bytes == NULL || fread(bytes, 1, (size_t)length, file) != (size_t)length)
    fail("cannot read Wasm module");
  fclose(file);
  *size = (size_t)length;
  return bytes;
}

static uint64_t run_calls(wasmtime_context_t *context,
                          const wasmtime_func_t *function,
                          wasmtime_val_raw_t *values,
                          const wasmtime_val_raw_t *original, size_t nargs,
                          size_t value_count,
                          uint64_t iterations) {
  uint64_t started = now_ns();
  for (uint64_t i = 0; i < iterations; i++) {
    for (size_t arg = 0; arg < nargs; arg++)
      values[arg] = original[arg];
    wasm_trap_t *trap = NULL;
    wasmtime_error_t *error = wasmtime_func_call_unchecked(
        context, function, values, value_count, &trap);
    check_error(error, trap);
  }
  return now_ns() - started;
}

static uint64_t calibrate(wasmtime_context_t *context,
                          const wasmtime_func_t *function,
                          wasmtime_val_raw_t *values,
                          const wasmtime_val_raw_t *original, size_t nargs,
                          size_t value_count,
                          uint64_t target_ns) {
  uint64_t iterations = 1;
  for (;;) {
    uint64_t elapsed = run_calls(context, function, values, original, nargs,
                                 value_count, iterations);
    if (elapsed >= target_ns / 10 || iterations >= (1ull << 40)) {
      if (elapsed == 0)
        return iterations;
      long double scaled = (long double)iterations * (long double)target_ns /
                           (long double)elapsed;
      return scaled < 1 ? 1 : (uint64_t)scaled;
    }
    iterations *= 10;
  }
}

static void instantiate_once(wasm_engine_t *engine,
                             const wasmtime_module_t *module) {
  wasmtime_store_t *store = wasmtime_store_new(engine, NULL, NULL);
  if (store == NULL)
    fail("cannot create instantiation store");
  wasmtime_context_t *context = wasmtime_store_context(store);
  wasmtime_linker_t *linker = wasmtime_linker_new(engine);
  if (linker == NULL)
    fail("cannot create instantiation linker");
  check_error(wasmtime_linker_define_unknown_imports_as_default_values(
                  linker, context, module),
              NULL);
  wasmtime_instance_t instance;
  wasm_trap_t *trap = NULL;
  check_error(wasmtime_linker_instantiate(linker, context, module, &instance,
                                          &trap),
              trap);
  wasmtime_linker_delete(linker);
  wasmtime_store_delete(store);
}

static uint64_t run_instantiations(wasm_engine_t *engine,
                                   const wasmtime_module_t *module,
                                   uint64_t iterations) {
  uint64_t started = now_ns();
  for (uint64_t i = 0; i < iterations; i++)
    instantiate_once(engine, module);
  return now_ns() - started;
}

static uint64_t calibrate_instantiations(wasm_engine_t *engine,
                                         const wasmtime_module_t *module,
                                         uint64_t target_ns) {
  uint64_t iterations = 1;
  for (;;) {
    uint64_t elapsed = run_instantiations(engine, module, iterations);
    if (elapsed >= target_ns / 10 || iterations >= (1ull << 30)) {
      if (elapsed == 0)
        return iterations;
      long double scaled = (long double)iterations * (long double)target_ns /
                           (long double)elapsed;
      return scaled < 1 ? 1 : (uint64_t)scaled;
    }
    iterations *= 10;
  }
}

int main(int argc, char **argv) {
  const char *module_path = NULL, *init_name = NULL, *export_name = NULL;
  const char *args_text = NULL, *out_path = NULL;
  int round = 0;
  bool measure_instantiate = false;
  uint64_t target_ns = 100000000;
  for (int i = 1; i < argc; i++) {
    if (strcmp(argv[i], "-module") == 0 && ++i < argc)
      module_path = argv[i];
    else if (strcmp(argv[i], "-init") == 0 && ++i < argc)
      init_name = argv[i];
    else if (strcmp(argv[i], "-export") == 0 && ++i < argc)
      export_name = argv[i];
    else if (strcmp(argv[i], "-args") == 0 && ++i < argc)
      args_text = argv[i];
    else if (strcmp(argv[i], "-round") == 0 && ++i < argc)
      round = atoi(argv[i]);
    else if (strcmp(argv[i], "-benchtime-ns") == 0 && ++i < argc)
      target_ns = strtoull(argv[i], NULL, 10);
    else if (strcmp(argv[i], "-out") == 0 && ++i < argc)
      out_path = argv[i];
    else if (strcmp(argv[i], "-measure-instantiate") == 0)
      measure_instantiate = true;
    else
      fail("invalid arguments");
  }
  if (module_path == NULL || export_name == NULL || out_path == NULL ||
      target_ns == 0)
    fail("-module, -export, -out, and positive -benchtime-ns are required");

  size_t wasm_size = 0;
  uint8_t *wasm = read_file(module_path, &wasm_size);
  wasm_config_t *config = wasm_config_new();
  wasmtime_config_cranelift_opt_level_set(config, WASMTIME_OPT_LEVEL_SPEED);
  wasmtime_config_cranelift_flag_set(config, "regalloc_algorithm",
                                     "backtracking");
  wasm_engine_t *engine = wasm_engine_new_with_config(config);
  if (engine == NULL)
    fail("cannot create engine");
  wasmtime_module_t *module = NULL;
  check_error(wasmtime_module_new(engine, wasm, wasm_size, &module), NULL);
  free(wasm);
  FILE *output = fopen(out_path, "a");
  if (output == NULL)
    fail("cannot open output");
  if (measure_instantiate) {
    instantiate_once(engine, module);
    uint64_t instantiate_iterations =
        calibrate_instantiations(engine, module, target_ns);
    uint64_t instantiate_elapsed =
        run_instantiations(engine, module, instantiate_iterations);
    fprintf(output,
            "{\"engine\":\"cranelift\",\"stage\":\"instantiate\","
            "\"module\":\"%s\",\"round\":%d,\"iterations\":%" PRIu64
            ",\"elapsed_ns\":%" PRIu64 ",\"ns_per_op\":%.9Lf}\n",
            module_path, round, instantiate_iterations, instantiate_elapsed,
            (long double)instantiate_elapsed /
                (long double)instantiate_iterations);
    fflush(output);
  }
  wasmtime_store_t *store = wasmtime_store_new(engine, NULL, NULL);
  wasmtime_context_t *context = wasmtime_store_context(store);
  wasmtime_linker_t *linker = wasmtime_linker_new(engine);
  check_error(wasmtime_linker_define_unknown_imports_as_default_values(
                  linker, context, module),
              NULL);
  wasmtime_instance_t instance;
  wasm_trap_t *trap = NULL;
  check_error(wasmtime_linker_instantiate(linker, context, module, &instance,
                                          &trap),
              trap);

  wasmtime_extern_t item;
  if (init_name != NULL && init_name[0] != '\0') {
    if (!wasmtime_instance_export_get(context, &instance, init_name,
                                      strlen(init_name), &item) ||
        item.kind != WASMTIME_EXTERN_FUNC)
      fail("initialization export is not a function");
    trap = NULL;
    check_error(wasmtime_func_call(context, &item.of.func, NULL, 0, NULL, 0,
                                   &trap),
                trap);
  }
  if (!wasmtime_instance_export_get(context, &instance, export_name,
                                    strlen(export_name), &item) ||
      item.kind != WASMTIME_EXTERN_FUNC)
    fail("benchmark export is not a function");
  wasmtime_func_t function = item.of.func;
  wasm_functype_t *type = wasmtime_func_type(context, &function);
  const wasm_valtype_vec_t *params = wasm_functype_params(type);
  const wasm_valtype_vec_t *returns = wasm_functype_results(type);

  size_t value_count = params->size > returns->size ? params->size : returns->size;
  wasmtime_val_raw_t *values = calloc(value_count, sizeof(*values));
  wasmtime_val_raw_t *original = calloc(value_count, sizeof(*original));
  char *args_copy = strdup(args_text == NULL ? "" : args_text);
  char *save = NULL;
  size_t nargs = 0;
  for (char *part = strtok_r(args_copy, ",", &save); part != NULL;
       part = strtok_r(NULL, ",", &save)) {
    if (nargs >= params->size)
      fail("too many arguments");
    switch (wasm_valtype_kind(params->data[nargs])) {
    case WASM_I32:
      original[nargs].i32 = (int32_t)strtol(part, NULL, 10);
      break;
    case WASM_I64:
      original[nargs].i64 = (int64_t)strtoll(part, NULL, 10);
      break;
    case WASM_F32:
      original[nargs].f32 = strtof(part, NULL);
      break;
    case WASM_F64:
      original[nargs].f64 = strtod(part, NULL);
      break;
    default:
      fail("unsupported argument type");
    }
    nargs++;
  }
  if (nargs != params->size)
    fail("argument count mismatch");

  run_calls(context, &function, values, original, nargs, value_count, 1);
  uint64_t iterations = calibrate(context, &function, values, original, nargs,
                                  value_count, target_ns);
  uint64_t elapsed = run_calls(context, &function, values, original, nargs,
                               value_count, iterations);
  fprintf(output,
          "{\"engine\":\"cranelift\",\"stage\":\"exec\",\"module\":\"%s\","
          "\"export\":\"%s\",\"round\":%d,\"iterations\":%" PRIu64
          ",\"elapsed_ns\":%" PRIu64 ",\"ns_per_op\":%.9Lf}\n",
          module_path, export_name, round, iterations, elapsed,
          (long double)elapsed / (long double)iterations);
  fclose(output);

  free(args_copy);
  free(values);
  free(original);
  wasm_functype_delete(type);
  wasmtime_linker_delete(linker);
  wasmtime_store_delete(store);
  wasmtime_module_delete(module);
  wasm_engine_delete(engine);
  return 0;
}
