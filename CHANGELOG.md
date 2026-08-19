# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- Agents now communicate with the orchestrator exclusively over HTTP — WebSocket dispatch has been removed
- Agent poll loop: agents claim jobs, send heartbeats, and submit results via HTTP endpoints (`POST /agent/jobs/claim`, `/heartbeat`, `/ack`, `/result`)
- Orchestrator dispatcher refactored into a pure reaper — it no longer claims or dispatches jobs, only reaps expired claims on a timer
- `AGENT_ORCHESTRATOR_URL` replaces `AGENT_ORCHESTRATOR_WS_URL`; value is an HTTP(S) base URL (e.g. `http://orchestrator:8080`)

### Removed

- WebSocket endpoint (`GET /ws`) and all associated agent session infrastructure (`pkg/agentsession`, `pkg/wsproto`)
- Protobuf envelope definitions (`proto/`, `pkg/terraplane/v1/`) and `protoc-gen` Makefile target
- `ORCHESTRATOR_AGENT_PING_INTERVAL`, `ORCHESTRATOR_AGENT_PONG_TIMEOUT`, `ORCHESTRATOR_AGENT_MISSED_HEARTBEATS` config fields


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
