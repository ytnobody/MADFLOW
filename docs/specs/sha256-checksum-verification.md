# SHA256 Checksum Verification for Binary Auto-Update

## Overview

When `madflow upgrade` downloads a binary from GitHub Releases, it must verify the SHA256 checksum of the downloaded file before replacing the current executable. This prevents malicious binary replacement in the event of a GitHub Release compromise or a man-in-the-middle (MITM) attack.

## Related Issue

- Issue: [#200 バイナリ自動更新時にSHA256チェックサム検証を追加する](https://github.com/ytnobody/MADFLOW/issues/200)
- Security Audit: `SECURITY_AUDIT_REPORT.md` section 3.3

## Design

### Checksum File Format

For each binary release asset, a corresponding SHA256 checksum file is published alongside it. The checksum file name follows the pattern:

```
{binary-name}.sha256
```

For example:
- Binary: `madflow-linux-amd64`
- Checksum: `madflow-linux-amd64.sha256`

The content of the checksum file is a single line containing the lowercase hex-encoded SHA256 digest of the binary, followed by a newline:

```
<hex-sha256-digest>
```

This format is intentionally simple (digest only, no filename), compatible with standard `sha256sum` output when trimmed.

### Verification Flow

1. Fetch the latest release info from the GitHub Releases API.
2. Locate the binary asset matching the current platform (`madflow-{os}-{arch}`).
3. Locate the checksum asset for that binary (`madflow-{os}-{arch}.sha256`).
4. If the checksum asset is not found in the release, abort with an error.
5. Download the binary to a temporary file.
6. Download the checksum file (in memory).
7. Compute the SHA256 digest of the downloaded temporary binary.
8. Compare the computed digest with the expected digest from the checksum file.
9. If they do not match, delete the temporary file and abort with an error.
10. If they match, proceed with replacing the current executable.

### Error Handling

- If the checksum asset is not present in the release, the upgrade is aborted. This makes checksum verification mandatory (fail-closed).
- If the downloaded checksum content is malformed (empty, unexpected format), the upgrade is aborted.
- If the checksums do not match, the upgrade is aborted and the temporary binary is deleted.

## Release Workflow Changes

The `.github/workflows/release.yml` workflow is updated to generate and upload checksum files for each binary artifact:

```bash
cd dist
for f in madflow-linux-amd64 madflow-linux-arm64 madflow-darwin-amd64 madflow-darwin-arm64 madflow-windows-amd64.exe; do
  sha256sum "$f" | awk '{print $1}' > "${f}.sha256"
done
```

The resulting `.sha256` files are uploaded to the GitHub Release alongside the binary files.

## Security Properties

- **Integrity**: Any modification to the binary after release will cause checksum mismatch and the upgrade will be rejected.
- **Fail-closed**: If a checksum file is missing from the release, the upgrade is refused rather than proceeding without verification.
- **No trust-on-first-use**: The checksum file and binary are both fetched from the same GitHub Release; the verification catches corruption or tampering of the binary relative to the published checksum.

## Limitations

- This does not provide cryptographic authenticity (i.e., it does not prove the release was made by the legitimate maintainer). For stronger guarantees, GPG signing of checksums would be needed.
- Both the binary and checksum file are served from GitHub. A full GitHub Release compromise would affect both. However, this protects against MITM attacks on the download transport and accidental binary corruption.
