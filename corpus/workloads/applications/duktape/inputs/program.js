function isPrime(n) {
  if (n < 2) return false;
  for (var d = 2; d * d <= n; d++) if (n % d === 0) return false;
  return true;
}
var primes = [];
for (var n = 2; n <= 500; n++) if (isPrime(n)) primes.push(n);
var totals = primes.reduce(function (out, n) {
  var key = String(n % 10);
  out[key] = (out[key] || 0) + n;
  return out;
}, {});
console.log(primes.length, primes.reduce(function (a, b) { return a + b; }, 0), totals[1], totals[2], totals[3], totals[5], totals[7], totals[9]);
