#include "support.h"

int main(void) {
  initialise_benchmark();
  int result = benchmark();
  int verified = verify_benchmark(result);
  return verified == 1 ? 0 : (verified < 0 ? 2 : 1);
}
