#include <stdint.h>
#include <string.h>
#include <time.h>
#include "wren.h"

clock_t wago_clock(void) { return 0; }

static int had_error;
static void write_fn(WrenVM *vm, const char *text) { (void)vm; (void)text; }
static void error_fn(WrenVM *vm, WrenErrorType type, const char *module, int line, const char *message) {
    (void)vm; (void)module; (void)message; had_error = 100000 + (int)type * 10000 + line;
}

uint64_t wren_run(void) {
    static const char source[] =
        "class Bench {\n"
        "  static run {\n"
        "    var primes = []\n"
        "    for (n in 2..1200) {\n"
        "      var prime = true\n"
        "      for (p in primes) {\n"
        "        if (p * p > n) break\n"
        "        if (n % p == 0) {\n"
        "          prime = false\n"
        "          break\n"
        "        }\n"
        "      }\n"
        "      if (prime) primes.add(n)\n"
        "    }\n"
        "    var acc = 0\n"
        "    for (i in 0...primes.count) { acc = (acc + primes[i] * (i + 3)) % 1000000007 }\n"
        "    return acc + primes.count * 1000000\n"
        "  }\n"
        "}\n";
    WrenConfiguration config; wrenInitConfiguration(&config);
    config.writeFn = write_fn; config.errorFn = error_fn; had_error = 0;
    WrenVM *vm = wrenNewVM(&config);
    if (!vm) return 1;
    if (wrenInterpret(vm, "main", source) != WREN_RESULT_SUCCESS || had_error) {
        uint64_t error = (uint64_t)had_error; wrenFreeVM(vm); return error;
    }
    wrenEnsureSlots(vm, 1); wrenGetVariable(vm, "main", "Bench", 0);
    WrenHandle *call = wrenMakeCallHandle(vm, "run");
    if (!call || wrenCall(vm, call) != WREN_RESULT_SUCCESS || had_error) { if (call) wrenReleaseHandle(vm, call); wrenFreeVM(vm); return 3; }
    double value = wrenGetSlotDouble(vm, 0);
    wrenReleaseHandle(vm, call); wrenFreeVM(vm);
    return (uint64_t)value;
}
