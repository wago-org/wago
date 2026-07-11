# Plugin API plan

The pre-release plugin API was redesigned around explicit host capabilities.
The current contract, manifest schema, load ordering, provenance rules, and
core-size boundary are documented in [plugin-api-v2.md](plugin-api-v2.md).

Workers are the first consumer of the managed-instance capability; see
[plugin-workers.md](plugin-workers.md). Pooling policy has been removed from the
core runtime and is reserved for a future plugin built on the same restricted
instance-management mechanism.
