# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed
- Agents poll the orchestrator over HTTP for jobs instead of holding a WebSocket. While a job is running the agent heartbeats to renew the claim lease; expired claimed and running jobs return to pending.

### Removed
- Agent WebSocket endpoint, in-memory session registry, and ping/pong protocol.


## [0.2.0] - 2026-08-18

### Changed
- Orchestrators are now able to be "HA". They periodically poll the DB for enqueued jobs for connected agents and dispatch them. As long as an agent is connected to an orchestrator it will receive commands.


## [0.1.4] - 2026-08-10

### Changed
- `terraplane.yaml` is now environment-scoped: stacks live under `environments`, agents can be set per environment (optional stack override), and stack names must be globally unique
- Comment commands accept `-e` / `-env` to plan, apply, or unlock all stacks in an environment (combine with `-s` for intersection)
- PR result comments: terraplane-branded layout with stack header, dir/commit meta, add/change/destroy table, and collapsed output (less Atlantis-like)
- Valid comments will receive a reaction from Terraplane to acknowledge receipt
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
