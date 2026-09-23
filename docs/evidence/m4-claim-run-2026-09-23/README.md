# Bounded claim/release execution evidence — 2026-09-23

Case: `ledger-claim-release-v1`.
Fixture: `f59af8adfe82573b64b8dfda427a0b00`.
Executed commit: `b4bda19f07bb22f37737508cd10424b75690d6f5`.

**PASS:** nine claim/release transitions, 46 authority ACL denials and 33 observed
counter values per snapshot passed validation. The 28-entry action journal
includes 11 cleanup actions. All six role credentials were revoked; separate
direct checks confirmed all four containers and both volumes absent.

This package retains exact controller report, immutable intent and action-journal
bytes, the separate owner execution decision, and a post-run validation/absence
check. Each export matches its private original byte for byte.

| Artifact | SHA-256 |
|---|---|
| `f59af8adfe82573b64b8dfda427a0b00.json` | `a2e16e91a190df09cee6fe6c76a7365a4f0e73632a7e354c593111878b62ff0b` |
| `f59af8adfe82573b64b8dfda427a0b00.intent.json` | `d0d1d8deec21d18f35182133c644d0e81c32390337627aca937289154c1e0f97` |
| `f59af8adfe82573b64b8dfda427a0b00.actions.jsonl` | `22184c47bc4e57e3457ce9fbb318f49736b28a25dad7adc14742145d0b30faa5` |
| `execution-decision.json` | `4024dd05473fef434110d9a5764dd6fe3b97d8965f3abdf9c3c7e45830b81792` |
| `postcheck.json` | `af76435ed4b504b0f2f2a3694228f773de48657eb6f75c6a5e774bdd1f263abd` |

The final report is 48,203 bytes and was written at 13:25:53.880 UTC. The separate
postcheck completed at 13:32:38.858454 UTC. A scoped redacted Gitleaks scan of the
exported evidence found no leaks. See the
[dated result](../../crawl-jobs-v2-m4-claim-run-2026-09-23.md) for authorization,
observations and evidence limits.

The original private files remain outside Docker volumes at:

```text
/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-claim-execution-2026-09-23/
```

The intent's `INCOMPLETE` status records its pre-mutation state; the final report
binds its hash and records the completed result. Approval
`bb116f3d918a000388b246f2caad1340a424e4f281c0d0cb9e47f03b8c3832cc`
is consumed; its private reservation and final disposition bind this fixture and
report, with `reusable=false`. A passing bounded claim/release case does not
establish full M4 acceptance (`m4_accepted=false`).
