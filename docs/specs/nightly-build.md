# Nightly Build Specification

## Overview

The nightly build workflow automatically creates build artifacts from the `develop` branch
when new commits exist since the last nightly build.

## Trigger

- **Schedule**: Daily at JST 0:00 (UTC 15:00)
- **Manual**: Can also be triggered manually via `workflow_dispatch`

## Behavior

1. The workflow checks whether the `nightly` tag exists on GitHub.
2. If the `nightly` tag exists and points to the same commit as the HEAD of `develop`,
   the workflow exits early with no-op (no new build is needed).
3. If the `nightly` tag does not exist, or if it points to a different commit than
   the HEAD of `develop`, the workflow proceeds to build.

## Build Process

1. Checkout the `develop` branch.
2. Build binaries for all supported platforms:
   - `linux/amd64`
   - `linux/arm64`
   - `darwin/amd64`
   - `darwin/arm64`
   - `windows/amd64`
3. Generate SHA256 checksums for each binary.
4. Move or create the `nightly` tag to point to the current HEAD of `develop`.
5. Create or update the GitHub Release for the `nightly` tag with all artifacts.

## Tag Management

- The `nightly` tag is a **mutable, force-pushed tag** that always points to the
  latest nightly build commit on `develop`.
- The release associated with the `nightly` tag is updated (pre-release) with each
  new nightly build.

## Artifact Naming

Binaries follow the same naming convention as release builds:
- `madflow-linux-amd64`
- `madflow-linux-arm64`
- `madflow-darwin-amd64`
- `madflow-darwin-arm64`
- `madflow-windows-amd64.exe`
- Corresponding `.sha256` checksum files

## Version Embedding

The build version is embedded as:
```
nightly-<YYYYMMDD>-<short-SHA>
```

For example: `nightly-20260412-a1b2c3d`
