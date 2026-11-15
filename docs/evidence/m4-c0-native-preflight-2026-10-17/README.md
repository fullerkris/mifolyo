# C0-ARCH-05 host discovery and preflight evidence

Scope: read-only host selection and offline preflight implementation. Native
actor admission, ptrace, collector provisioning/authentication and calibration
have not occurred. See the [checkpoint](../../crawl-jobs-v2-m4-c0-native-preflight-2026-10-17.md).

## Initial records

- [Initial SSH discovery](initial-ssh-discovery.json): owner-selected
  `moogle-main`; missing collector paths, Yama scope 1, successful read-only
  Docker metadata and both tool classification errors retained. These are
  transcribed SSH tool results, not authenticated context-worker receipts.
- [Actual local preflight](local-preflight.json), [stderr](local-preflight.stderr):
  `BLOCKED`, exit 1, missing Docker and collector. Local scope 0 is not the remote
  scope-1 observation. No remote execution of the new CLI is claimed.
- [Source inventory](source-inventory.json) and [source patch](initial-source.patch):
  first frozen 57-file package plus two scripts and initial report; all 60 source
  hashes bound to review inventory `aa84e8b9…`. The patch also records the roadmap
  update as it stood at first export.
- [Source-bound tests](tests-022.json) and [log](tests-022.log): 302 passed,
  including all 288 earlier IDs, with no skips or expected failures.
- [PR #32](pr32.json), [checks](pr32-checks.json), [image workflow](pr32-images-run.json):
  published head `babb218`, open PR, native image workflow success on attempt 1.
  CLI captures preserve commands, timestamps, stdout/stderr and exit codes:
  [PR](pr32-capture.json), [checks](pr32-checks-capture.json),
  [image run](pr32-images-run-capture.json). These are status records, not
  downloaded-artifact or merged-main verification.
- [Initial manifest](initial-manifest.json): 13 original exports and exact hashes.

The historical records above are immutable. Review follow-ups and final status
are added separately. Durable initial copy:
`out/c0-native-preflight-2026-10-17/initial-aa84e8b93214/`.

## Initial independent correctness review

[Report](initial-correctness/report.md), [machine-readable result](initial-correctness/result.json)
and [exact export manifest](initial-correctness-manifest.json) retain **COR-01**
(unstructured child-wait timeout) and **COR-02** (valid no-new-privileges metadata
rejected). The original four failing architecture subtests, all probe sources,
15 independent passing controls and the earlier corrected test-harness failure
remain under `initial-correctness/`. The initial verdict is NEEDS_REMEDIATION;
later repairs do not rewrite this historical result.

## Correctness remediation and initial security review

- [Correctness re-review](correctness-remediation/report.md) and
  [manifest](correctness-remediation-manifest.json): scoped GO on the intermediate
  `4046bd7f…` inventory, closing COR-01/02. The intermediate source inventory and
  304-test records are retained as `remediation-*.json` / `remediation-*.log`.
- [Initial security review](initial-security/SECURITY-REVIEW.md),
  [summary](initial-security/review-summary.json) and [manifest](initial-security-manifest.json):
  NEEDS_REMEDIATION for S1 (implicit CLI config/plugin behavior), S2 (timeout) and
  S3 (normal-return descendant). All original probe outcomes and the dismissed
  overstrict JSON test are retained. Reviewer-owned descendant cleanup is recorded.

## Replacement transport

[Source inventory](transport-source-inventory.json) binds the fixed Unix-socket
reader and integration to `faf8d794…`: 59 package files, two scripts and checkpoint.
It permits only GET info/version, without Docker CLI, config/plugins, proxy
environment, DNS or subprocesses. The intermediate correctness GO above is not
reused as a final verdict for this new transport.

- [318-test source-bound result](transport-tests-022.json) and [log](transport-tests-022.log):
  all 304 previous IDs retained, no skips/failures; 14 new transport tests.
- [30 focused tests](security-remediation-targeted.log): both architectures,
  parsing/framing/bounds, config/plugin canaries and real owned socket EOF/timeouts.
- [Final-code local probe](transport-local-preflight.json) and
  [stderr](transport-local-preflight.stderr): BLOCKED, exit 1, Docker socket and
  collector absent. The replacement reader was not run against a remote daemon.

Final-source correctness [report](final-correctness/report.md),
[result](final-correctness/result.json) and [manifest](final-correctness-manifest.json)
record **GO_FOR_CONTINUED_IMPLEMENTATION** with no findings: 30 focused and 15
additional controls pass independently; supplied 318-test evidence and all 62
snapshot paths verify.

Final-source security [report](final-security/SECURITY-REVIEW.md),
[result](final-security/review-summary.json) and [manifest](final-security-manifest.json)
also record **GO_FOR_CONTINUED_IMPLEMENTATION**. S1/S2/S3 are closed for preflight:
30 focused / 16 independent tests, 600 wire mutations and eight deep-JSON cases
pass. The unchanged live runtime CLI adapter is outside this certification.

[Final scoped gate](final-gate.json) binds the source, both final verdicts,
318-test evidence, finding closures and pending native-runtime obligations.
Gate SHA-256: `2e91337ea1d30d2d319c9b86019f36c7af75677e01a082d88660d37ad22fd7e8`.
[Continuity record](transport-continuity.json) retains the preceding preservation
checks; the final gate additionally verifies all 89 historical/reviewer manifest
entries. Durable reviewed snapshot: `out/c0-native-preflight-2026-10-17/transport-faf8d794802f/`.
Final delivery: `out/c0-native-preflight-2026-10-17/final-faf8d794802f/`.
The local `.gitignore` explicitly includes retained test/review logs in the
publication candidate; final integrity checks compare every export path with
Git's visible candidate, as well as verifying bytes in the durable copy.
