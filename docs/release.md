# Cutting a release

1. Merge the work you want in the release to `main`. Keep user-facing notes under `## [Unreleased]` in [CHANGELOG.md](../CHANGELOG.md) as you go (or fill them in now).
2. Open a PR (or commit) that moves `Unreleased` into a dated version section, e.g. `## [0.2.0] - YYYY-MM-DD`, and leaves an empty `## [Unreleased]` above it.
3. After that lands on `main`, tag the commit and push:

```bash
git checkout main
git pull
git tag v0.2.0
git push origin v0.2.0
```

4. The [Release](../.github/workflows/release.yml) workflow builds and publishes:
   - Binaries on the GitHub Release
   - `ghcr.io/terraplane-org/terraplane:<version>` (also `:<major.minor>` and `:latest`)
   - Helm chart to `oci://ghcr.io/terraplane-org/charts/terraplane`

5. After the **first** release, open the package(s) under the org’s Packages page and set visibility to **Public** if pulls should work anonymously.
