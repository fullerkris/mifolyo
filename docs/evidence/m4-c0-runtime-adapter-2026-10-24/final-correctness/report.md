# MiFolyo C0 R1 correctness re-review — 2026-10-24

## Verdict: GO_FOR_CONTINUED_IMPLEMENTATION

**R1 is closed for the scoped adapter-hardening review.** No new correctness
finding was identified in the remediation. The fix is small and appropriately
conservative: it rejects short-option spellings instead of attempting to
reimplement Docker's clustered-option parser, while retaining the exact fixed
oracle interpreter tail. This is not full-runner, native-runtime, host, or
installed-CLI-version admission, nor a separate security-review sign-off.

## Binding and preservation

Reviewed the frozen `remediation/source/`, not an unbound working-tree revision.

| Item | SHA256 |
|---|---|
| New `remediation/review-inputs.json` | `b3e66f6bf5cc95194e04548b7cfb4df4978898d4cec1a48aaaa357a1d4be14e7` |
| New `remediation/source-inventory.json` | `5a3bbce905d4e49d6b4a89e4492222e5aa8bf2b2d0fa1ad92333f189c585b7db` |
| Final `docker_backend.py` | `7a3aaaf2de8c3dd79f6b749dfbed00fcf35ed052d62dc20246bd66fe91d19e9c` |
| Final `test_live_adapter.py` | `6607d0879870d36b3b0573273896416aa9080a6523e6daefdd86f539f4c79e93` |

Before and after verification both passed:

- All **444 packet-bound files** matched their recorded bytes and hashes.
- All **63 final source/script files** matched the authoritative inventory;
  corresponding working source files matched too, with no extra frozen files.
- Only `docker_backend.py` and `test_live_adapter.py` differ from the initial
  source inventory. AST comparison isolates the product change to
  **`cli_arguments` only**. `streams.py` and `local_process.py` remain byte-exact.
- The initial correctness report/result/manifest still match their previously
  recorded hashes. No initial failed probe or report was changed.
- Exported `initial-correctness/` (**27 files**) and `initial-security/`
  (**13 files**) exactly match their original review directories, including
  filenames and file hashes. Both checks were repeated after testing.

Evidence: `integrity-before.json`, `integrity-after.json`, and
`coverage-crosscheck.json`. The final manifest binds this new review's files.

## R1 disposition

**Resolved location:** final `docker_backend.py:39–63`, especially `49–55`.

The old blacklist still rejects forbidden long options and command tokens.
The added check rejects every token starting with one ASCII dash but not two,
including clustered help and attached host/context forms. It executes before
`verify_cli()`, temporary-config creation, and Popen in `command()`
(`102–117`) and `DockerProcessStream` (`81–90`).

The only short-token exception requires all of:

1. Non-actor `container create`, with at least eight operation arguments.
2. `--entrypoint`, `python3`, and a valid nonzero lowercase `sha256:` image ID
   at the exact required end-relative positions.
3. The exact final three tokens `-I`, `-B`, `/oracle/worker.py`.

Only those final three tokens are excluded from the scan. Earlier short flags,
additional short flags, changed or displaced tails, invalid images, other
operations, and actor-mode uses were refused in independent controls. Both
architectures' genuine typed oracle-create argv still pass.

The original `-hh`/`-Dh`/`-hD` and attached `-H`/`-c` controls now pass without
modification. The original finding's pinned Docker v29.0.0 parser/help evidence
is retained in the packet; no new Docker execution was used to establish this
closure. The conclusion remains about the adapter's rejection boundary, not a
claim that a prior remote-endpoint bypass had been demonstrated.

## Independent tests

All new executions used **`python3 -B -W error::ResourceWarning`**.

| Execution | Umask 022 | Umask 077 |
|---|---:|---:|
| Unchanged original independent probes, rebound to final source | **23/23 PASS** | **23/23 PASS** |
| New independent variant/compatibility test methods | **8/8 PASS** | **8/8 PASS** |
| Negative guard cases within new methods | 6,132 rejected | 6,132 rejected |
| Positive direct guard cases | 12 accepted | 12 accepted |
| Typed builtin argv checks, simulated, both architectures combined | 218 accepted | 218 accepted |
| Typed actor-start argv checks, simulated, both architectures combined | 6 accepted | 6 accepted |

**62 test-method executions passed, zero failures/errors/skips.** Table-driven
case counts are not additional test-method counts.

### Original probe replay

`replay_original.py` loads the unchanged initial `independent_probes.py` after
preloading the final frozen product modules. Only output/source-hash locations
and import bindings are redirected in memory; original test methods and files
are not edited. `replay-binding-022.json` and `replay-binding-077.json` record
the probe hash, wrapper hash, exact module paths/hashes, and the absence of
loaded initial product-source modules.

Besides both originally failing R1 tests, the replay retained all previous
passing lifecycle checks: exit-status preservation, exact output budget,
partial-constructor failures, descriptor closure, sticky cleanup errors,
repeated close, stale-reaped-PID signal refusal, config lifetime, and an owned
orphan witness killed before leader release. Only inert Python children ran.
The witness harness's subreaper setting was process-local and restored.

### New variants and compatibility

`variant_probes.py` independently covered:

- Short-flag clusters of lengths one through three, attached values, lone `-`,
  and additional spellings across every allowed builtin operation family.
- Actor-mode rejection and retained prefix, operation, long-option, type,
  length, NUL, and total-argument-size restrictions.
- Exact minimum-length oracle tails; malformed entrypoints, interpreter names,
  images, flags, worker paths, swapped/truncated/displaced/extended tails;
  extra short-token insertion at every argument boundary.
- Typed prepare/create/open/cleanup flows for **x86_64 and aarch64**, using
  in-memory metadata and fake streams. Every one of the ten allowed builtin
  operation families and all three actor roles was exercised; the oracle tail
  remained byte-exact and simulated resources were removed afterward.

Mocks asserted that negative variants never reached binary inspection, config
allocation, Popen, or metadata access. The new variant tests launched **no
subprocess at all**. Their temporary profile fixtures stayed inside this review
directory. Detailed negative cases and accepted typed argv are retained in
`variants-022.json` and `variants-077.json`; corresponding logs are separate.

## Broad-suite evidence cross-check

The existing new 022/077 reports each record **338/338 PASS**, with no skipped or
expected-failure cases. Their inventories match fresh frozen-source discovery;
all 61 package hashes and the runner hash match. All **335 initial test IDs**
are retained, including all **318 historical IDs**. Exactly three test methods
were added in remediation, bringing the total new since the historical baseline
to 20. All 17 original live-adapter test-method ASTs remain unchanged.

The broad 338-test suite was **not reexecuted by this reviewer**. Its source
bindings and inventory were checked independently instead. The historical
318-test report used for comparison is separately identified and hashed in
`coverage-crosscheck.json` rather than newly claimed as a packet member.

## Unchanged limits and next steps

- No Docker, SSH, native actors, calibration, or other agents were invoked.
  No product code, Git state, original reviews, or existing evidence was changed.
  All new writes are confined to `remediation/correctness-review/`.
- Original config controls still verify the requested explicit `/tmp` but
  redirect actual private directories into this review directory. This is not
  a test of the real `/tmp` filesystem. No actual Docker binary was admitted.
- Neither the original pinned v29 documentation nor this guard re-review admits
  an installed CLI binary/version or every historical/future CLI implementation.
- Local group signalling remains distinct from proof of all escaped descendants,
  all process-group members, or remote-container absence. The explicit false
  proof flags remain unchanged and were checked again in the original replay.
- Host provider/hypervisor details are still awaited. The recorded x64 host
  assessment remains scoped, with Yama 1/collector blockers and no fresh full
  preflight or daemon/kernel/namespace pairing. No VM or collector was created.
- Native calibration, full-runner/native readiness, and ARM64 native admission
  remain deferred/unproven. Simulated aarch64 argv compatibility is not native
  ARM64 execution evidence.

The remediation satisfies R1 without broadening its change surface. Continue
implementation with the host, installed-CLI, and native acceptance gates intact.
