No actual flaw found in the fixture change. The added `struct.new_default` writes to global 8, so the allocator is observable. The `Required && Exact` check makes a missing root map fail clearly. The test still checks both artifact paths, trap results, and dirty globals.

I inspected the source only. I did not build or run tests.