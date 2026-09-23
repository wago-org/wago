# Shared JavaScript runtime workload

QuickJS and Duktape execute the same ES5-compatible program so their command
benchmarks compare interpreter work rather than different algorithms. The
program generates and parses 50,000 synthetic service events, aggregates 97
service buckets, sorts the results, and reports a deterministic checksum.

The event stream is generated in the guest from a fixed xorshift seed. This
keeps the checked-in input small while exercising allocation, string handling,
regular expressions, arrays, objects, numeric operations, sorting, and garbage
collection in both interpreters.
