# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed
- Docker CI/release builds use Buildx GitHub Actions cache; CI no longer uses `load: true`
- Dockerfile fetches Atlas before copying sources so code changes do not re-download it

## [0.1.3] - 2026-08-08

### Added
- GitHub Actions release workflow: semver tags publish GHCR images, Helm OCI charts, and binaries
- MIT license
- Release cutting docs (`docs/release.md`)

### Changed
- CI no longer pushes images/charts on every `main` commit; releases are tag-driven
- Default container/Helm registry moved to `ghcr.io/terraplane-org`
