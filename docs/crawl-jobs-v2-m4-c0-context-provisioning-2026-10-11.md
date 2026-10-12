# C0 trusted-source provisioning and pinned-SSH tooling — 2026-10-11

**Local tooling implementation verified; independent review and live host
installation remain open.** The owner selected **Pinned-SSH tooling**: implement
provisioning bundles, constrained SSH transport and controller preflight for an
operator-managed Linux/AArch64 Docker host. Live installation explicitly awaits
identification of that host. Work is local on
`feature/crawl-jobs-v2-c0-context-provisioning`, based on merged `64c98a7`.

## Merged baseline verified

PR #24 merged as `64c98a78e0e0ae1731f1516c06932bc881e8becf`, parent `2ff03af`,
with the exact approved `f842de5bed3aacd299c7be2bd49ad84d44dbfdf2` tree.
Its actual main workflows, Required Checks `38174952535` and Unit Tests
`38174952532`, both passed on attempt 1. Independent retained-evidence verification
accounts for 14 protected contexts / 23 jobs, 161 C0 / 31 script / 183 harness
tests, 480 race roots (479 passes and the existing optional native-Lua skip), all
11 M4 roots and 74-file / 20-recipe image evidence. Main CI gate:
`53159ebf13efec96b3d08dd491abbeadc8e3f84bc988c86dfff87baf1e01210e`.

## Selected deployment boundary

The current selected daemon reports Docker Desktop, LinuxKit `6.12.76-linuxkit`,
AArch64 and Docker `29.5.2`. Its installed Desktop CLI has no SSH/host-service
entrypoint, and no independently admitted collector endpoint in that VM has been
established. A source on a different Linux server cannot attest that daemon's
kernel/namespace context. The owner chose portable pinned-SSH tooling with live
installation deferred until the correct host is identified.

The Docker backend continues to select its existing local Unix socket. No daemon
switch, privileged container, namespace-entering workaround, production host
reuse, SSH host-key discovery/TOFU, host account creation or SSH service reload
was performed.

## Implemented

- **Private provisioning bundles:** a closed operator-supplied deployment binds
  the existing host anchor, numeric address/port, dedicated service UID/GID,
  independently verified Ed25519 host/client public keys and interpreter/client
  digests. Host and controller payloads are separated; secret values/hashes are
  absent from public manifests. Existing destinations are refused. Bundle
  validation checks exact source/config/policy/inventory and paired credentials.
- **Forced-command endpoint:** fixed command and marker, no caller paths,
  service identity, root-owned exact code inventory and interpreter checks,
  followed by the existing signed context protocol. Configuration reads now
  verify owned directories, bounded regular single-link files and stable identity.
- **Pinned bounded SSH:** fixed executable, isolated noninteractive configuration,
  pinned known-host data, no proxy/agent/local command/forwarding, numeric endpoint,
  private per-exchange credential copies and before/after credential/client checks.
  Nonblocking stdin/stdout/stderr handling enforces request/response bounds and
  deadlines, with bounded local process cleanup and value-free failures.
- **Controller preflight:** `acquire_provisioned_context()` joins the deployment
  and transport identities to preparation before actors activate, retaining the
  outer authorizers, selected-daemon checks and native Linux-expiry lease.
- **Operator contract:** the package README documents the private CLI, intended
  root/service ownership, fixed host paths, effective SSH policy validation and
  remaining live installation/admission steps. The staging tool is not a root
  installer or an execution-approval issuer.

The new preparation fields are `runtime_context_deployment` and
`runtime_context_transport`. The host source bundle now includes seven files.
Previous packets/bundle identities require regeneration; prior source GO does
not automatically cover this increment.

## Verification and identities

| Check | Result |
|---|---|
| C0 suite | **194 passes**, all 161 baseline IDs retained plus 33 additions; no skips/errors/expected failures or ResourceWarnings |
| Script suite / independent digest vectors | **31 passes / PASS** |
| Native and oracle artifact-only builds | **PASS**, source/manifest/ELF/compiled-BPF/profile bindings checked |
| Test mechanisms | Private files, simulated Linux/admission data, real benign Python pipes, real offline `ssh -G`; no live SSH authentication |
| Deployment/runtime | No host installation, real collector/actor sandbox or native calibration |

| Identity | SHA-256 |
|---|---|
| 45-file source inventory | `b69502b2287b08331570f7a7c2728586da216341ec9da0fdb0c097abfaac4c4a` |
| Local verification | `3e82982be4d22eeb7c0334fd77b3c5f28300b7d44c4da6fd025dd51d2c3a2019` |
| Artifact-only build verification | `c9826477cb4e8afbfbe3adb6c41112f1af87be97ebddf7e06a53e07b4cd2ec58` |
| Host source bundle | `00717364e862cad8012f42dc2702a3e3236822c861fbd21fd190b7e47fc78257` |
| Transport source bundle | `92b336c252cb61248cd3bbd10e28bda4422bb9107599232f26cbd4100a2e9b0d` |
| Oracle worker bundle | `a6c5fd8c2c42fceadff3c846f083b5f9ed92ffb93c4ea7e644bf9b0f315d71f9` |

See the [evidence index](evidence/m4-c0-context-provisioning-2026-10-11/README.md)
and [operator/tooling instructions](../tests/crawl-jobs-v2-observer/README.md#operator-provisioned-pinned-ssh-tooling).
Private records, clean baseline worktree and artifact outputs remain under
`/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-c0-context-provisioning-2026-10-11/`.

## Next gates

1. Independent correctness/security review of the new source/artifact snapshot,
   then a separate scoped publication decision.
2. Identify and admit an SSH-accessible Linux/AArch64 host in the selected Docker
   daemon's actual kernel domain. Review any future daemon-endpoint change separately.
3. Operator installation, effective SSH-policy admission, live authentication and
   real context acquisition; no deployment-complete claim until those pass.
4. Exact runtime-image/oracle OCI admission, full trial/timing/confinement and
   cleanup/absence assembly, and separately authorized calibration. Full M4 remains open.
