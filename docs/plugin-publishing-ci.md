# Publish a Wago plugin from CI

Use [`docs/examples/publish-wago-plugin.yml`](examples/publish-wago-plugin.yml)
as a starting point for a plugin repository's
`.github/workflows/publish-plugin.yml`.

The example has two paths:

- Pull requests run the plugin tests and `wago --dry-run --no-input plugin
  publish`. Dry-run validates `wago.json`, the plugin definitions, and the
  committed `wago.providers.json` without contacting the registry.
- Publishing a GitHub Release checks out its complete tag and runs `wago plugin
  publish` with `WAGO_NONINTERACTIVE=1`. The registry credential comes from the
  `WAGO_TOKEN` GitHub Actions secret and is never written to disk.

## Set up the repository

1. Add a repository or protected-environment Actions secret named
   `WAGO_TOKEN`. Its value must be a Wago registry API token, not the automatic
   `GITHUB_TOKEN` provided by Actions.
2. Copy the example to `.github/workflows/publish-plugin.yml` in the plugin
   repository.
3. Pin `WAGO_CLI_VERSION` to the Wago version used to validate the plugin.
4. Keep `package.version` in `wago.json` and the plugin definitions on the same
   version, then regenerate and commit `wago.providers.json`.
5. Push a matching version tag, such as `v1.2.3`, and publish a GitHub Release
   for that tag. Wago refuses to publish a missing, local-only, or mismatched
   tag.

Pull requests do not receive `WAGO_TOKEN`, and the validation job does not need
it. Only the release-only publish job reads the secret.

For a CI system other than GitHub Actions, the equivalent publication step is:

```sh
WAGO_TOKEN="$WAGO_TOKEN" WAGO_NONINTERACTIVE=1 wago plugin publish
```

Run it from a full checkout containing the pushed release tag.
