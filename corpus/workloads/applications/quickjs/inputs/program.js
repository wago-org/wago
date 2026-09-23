const values = Array.from({ length: 250 }, (_, i) => i + 1);
const primes = values.filter(n => n > 1 && values.slice(1, Math.floor(Math.sqrt(n))).every(d => n % d !== 0));
const grouped = primes.reduce((out, n) => {
  const key = String(n % 10);
  out[key] = (out[key] || 0) + 1;
  return out;
}, {});
const expression = /([a-z]+)(\d+)/g;
const matches = Array.from("alpha12 beta34 gamma56".matchAll(expression), match => match[1] + ":" + match[2]);
print(JSON.stringify({ count: primes.length, sum: primes.reduce((a, b) => a + b, 0), grouped, matches }));
