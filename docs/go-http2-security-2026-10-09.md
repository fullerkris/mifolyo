# Go HTTP/2 security update — 2026-10-09

This separately authorized security fix addresses **GO-2026-6617 /
CVE-2026-97032**, an HTTP/2 HPACK encoder race. The advisory was published at
2026-10-08T22:31:09Z, after documentation PR #21's successful PR CI. Its merged
main revision, `2bdb3e583065a487202dfa16dee438b29dce6abf`, subsequently failed the
PageRank and Spider vulnerability-scan steps. PageRank's vet and race tests passed
before its scan failed.

The original [PageRank failure](https://github.com/fullerkris/mifolyo/actions/runs/37867896393/job/113618936282)
and the [Go advisory](https://pkg.go.dev/vuln/GO-2026-6617) identify both the standard
library and `golang.org/x/net` as affected. The C0 publication candidate remains
separately preserved while this baseline fix is reviewed.

## Changes

| Input | Previous | Updated |
|---|---|---|
| Go toolchain, CI and Docker builders | 1.25.13 | **1.26.9** |
| Go module language requirement | 1.25.0 | **1.26.0**, required by the patched x/net |
| x/net, both services | v0.58.0 | **v0.60.0** |
| Spider x/sys | v0.47.0 | v0.48.0 |
| PageRank x/crypto / x/sync / x/text | v0.55.0 / v0.22.0 / v0.41.0 | v0.57.0 / v0.23.0 / v0.42.0 |

The same exact Go release is required by the existing differential-oracle tests.
Their bodies, selectors and assertions are unchanged apart from the version
literal and corresponding diagnostic/comment text. `go mod tidy` also correctly
classifies Spider's existing gopher-lua and x/text imports as direct dependencies;
the gopher-lua version does not change.

## Spider normalization compatibility

x/net v0.60.0 selects x/text v0.42.0, which changes NFC composition behavior and
the recomposition table's encoding. Applying those normalization changes would
alter the sealed CanonicalURLV1 profile. Spider therefore declares one exact,
version-specific replacement:

```go
replace golang.org/x/text v0.42.0 => golang.org/x/text v0.41.0
```

This keeps Spider's effective normalization implementation at its existing
version while its HTTP/2 implementation and Go standard library receive the
security fix. PageRank uses the newer x/text version normally. Neither scanner
configuration nor any advisory suppression is changed.

The Unicode generator requires Go 1.26.9 and the exact effective module versions,
checksums and replacement path. Missing, local-path or different replacements
reject. All 21 selected upstream Unicode-source files remain byte-identical.
The generator additionally requires the existing exhaustive oracle and Lua data
hashes before accepting regeneration. Its provenance records the new verifier
toolchain and replacement; the sealed data retains its historical identity.

| Sealed artifact | Unchanged SHA-256 |
|---|---|
| Exhaustive Go property oracle | `b728aa7ae09b7e577fead2845d3489daf7e20d427b2a23ef0ffcd7b1722d5981` |
| Unicode Lua data | `07243776f0a5b1691e872cf92bddaf60cebf292175f25e2146a65220f20e849e` |

The normative protocol, all canonical Lua bytes, digest vectors and Python URL
implementation retain their existing hashes. Historical execution/review records
retain their original source and toolchain bindings. Of the earlier 108-file
cancellation source inventory, only `services/spider/Dockerfile` changes; that old
inventory is historical evidence, not a new review of this security update.

## Local checks

- **govulncheck v1.7.0:** both services pass with no reachable or imported-package
  vulnerabilities. PageRank retains one module-only notice, GO-2026-5932, for the
  unimported `x/crypto/openpgp` package; it is not suppressed or treated as a
  reachable finding.
- **Six URL/Unicode race-test roots pass**, including real Lua lookups for all
  1,114,112 code points and independent Go/Python canonicalization checks.
- **Two real-generator negative probes pass:** removing the exact replacement or
  substituting a local path is refused before output validation.
- **Spider's other packages:** 219 top-level passes; the existing opt-in Redis and
  networkless Chromium integration tests skip locally because their fixtures are
  provided by hosted CI.
- **PageRank:** 13 passes; the MongoDB publication integration test similarly
  awaits its hosted fixture.
- **21 script tests**, independent digest verification, both services' `go vet`
  and `go mod verify`, and both Linux/amd64 builds pass.

The initial offline generator invocation encountered the Go launcher's checksum
requirement before execution. Invoking the already checksum-verified installed
Go 1.26.9 binary directly succeeded; the original failure log is retained.

Local verification SHA-256:
`a3f0e94bec58ec20e4d2082b24f9e8d91b45871112ae4412f4f8002509d09d20`.
Private logs and records are retained under
`/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/go-http2-security-2026-10-09/`.

This is the pre-publication local checkpoint. Exact security-PR CI must still
verify the full race inventory, service integrations, image preparation and
fourteen protected contexts. Merge is a separate decision. After the security
fix is merged and exact main CI is verified, the already-authorized C0 publication
can resume from that baseline. The 124-path C0 candidate, original design worktree
and unrelated primary-worktree changes are preserved.
