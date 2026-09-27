# P01 execution evidence — 2026-09-24

**PASS:** `ledger-candidate-compat-present-v1` / P01, fixture
`f3c66e07c31db6d3a141c083ba70c56d`, executed once on
`963b67b73e2cd67ff73f559fa9984813b9d5946a`.

| Artifact | SHA-256 |
|---|---|
| `f3c66e07c31db6d3a141c083ba70c56d.json` | `745ffa976a63b186f319c2f0d0ab2ff8df0b0e4c2b2044bd8e5b69da0fc710fe` |
| `f3c66e07c31db6d3a141c083ba70c56d.intent.json` | `ee5f23bd3962dfcc89147569f178fcd888c242adf68d87d42b6a426dfd97f88a` |
| `f3c66e07c31db6d3a141c083ba70c56d.actions.jsonl` | `4dd8081a57af9510579df8da0836e43baf6f5f612470ebd9a67ac51dbf214c23` |
| `execution-decision.json` | `121cfe778d7ec04742678ed385593f6bfe3f4a0bcdb7f35890ae7d4b1d3cc6e9` |
| `postcheck.json` | `ef021599e7f8efa33b220cdf205436f539ee6fcace88e6979f48a757d1b39e87` |
| `coverage.json` | `3663474dd2abf9bf200cb25e2b48bee5bd4d230004b4e6363b91439053379719` |

The first five files are exact copies of private mode-0600 originals. The intent
is deliberately `INCOMPLETE`: it names resources before mutation; the final
report supplies the outcome. `coverage.json` records this case's observed scope,
the 12 remaining unrun negatives and the next evidence-review gate.

Both CLAIM calls returned `CRAWL_V2_INVALID_STATE`; all 33 counters had zero
deltas and every retained post-state hash matched setup. All 46 exact authority
probes returned `NOPERM` without state change. Five stages passed two-process
isolation checks. Worker-first six-role revocation, 28 journal actions and
independent absence of all four containers/two volumes were verified.

Approval `d8eba81cc15e148c302f150bb96e40181ad4cde526c3dacc3f5354c964a5edc5`
is **consumed and non-reusable**. Approval, receipt, preflight, exclusive one-use
reservation and final disposition remain private. The run finished at
**18:04:40.906 UTC**, within its approval window and case budget.

Private originals and postcheck script:
`/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-p01-execution-2026-09-24/`.
The report/intent/journal are under `evidence/`; decision, postcheck and coverage
originals are in the workspace root.

See the [dated result](../../crawl-jobs-v2-m4-p01-run-2026-09-24.md).
Private full state was checked by the reviewed executor/controller; public
hashes do not reconstruct discarded private material. M4-P3/full M4 remain open,
with `m4_accepted=false`; another case needs fresh authority.
