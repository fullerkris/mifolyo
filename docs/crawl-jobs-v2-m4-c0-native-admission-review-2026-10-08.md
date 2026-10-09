# C0 native-runtime admission: independent review — 2026-10-08

**Correctness GO and security GO for continued implementation.** Independent
reviews of the remediated, frozen admission layer close all three correctness
and both security findings. No new actionable in-scope issue remains. This is
source/preparation approval; native runtime feasibility and full M4 remain open.

The branch remains `feature/crawl-jobs-v2-c0-observer`, based at `ab5f21a`.
The package now contains **26 files** and its final local suite passes **112 tests,
zero skips**, with ResourceWarnings treated as errors. The original implementation,
initial reviews, failed attempts and both immutable snapshots remain preserved.

## Findings and remediation

| Finding | Initial issue | Final closure |
|---|---|---|
| COR-01 / SEC-01 | Cached peer phase could permit `A`/`G` after readable EOF, stderr, partial or queued output; pre-G HELD could be accepted afterward | Bounded nonblocking live/quiet peer checks after the final authorizer and at the write boundary; failed transports and admission caches are invalidated. Collection checks live current/peer readiness after postflight work |
| COR-02 / SEC-02 | A slow successful authorizer could return after own or peer admission expiry, followed by a write | Own/peer expiries are checked after callbacks and polling, with a pre-write guard after the sender pump and a generic deadline check immediately before every write |
| COR-03 | Native uint64 monotonic nanoseconds exceeded the Python safe-integer range after roughly 104.25 days of host runtime | Exact uint64 clock validation for native CLOCK, trusted windows, hold endpoints and witness pre/post timestamps; counters and relative limits retain their old bounds |

The new permanent regressions use actual `ProcessStream` instances and benign
Python pipe peers with simulated Docker metadata. They check that refusal means
**zero attempted command bytes**, retain live positive controls, and cover peer-only
expiry and expiry during sender polling. No Docker inspection was added to A/G or
the HELD/resume guards. A live check is a point-in-time observation, not an atomic
guarantee of future peer lifetime or proof of remote absence.

## Independent verification

| Stream | Result |
|---|---|
| Correctness | Final 112-test suite passes; 15 independent probe tests pass, including 35 zero-write rejection scenarios and positive sequencing controls |
| Correctness static checks | 4,224 ELF-format/frame joins, including uint64-maximum clocks; manifest/filter/profile joins and both frozen snapshots verified |
| Security | 15 independent probe tests pass, including 34 zero-write dispatch refusals and four final-collection refusals |
| Security static checks | Two ELFs, all profiles, 4,680 bounded BPF simulations and 20 wire-template joins pass; scoped secrets scan reports no leaks |
| Source preservation | Both reviewers verify the exact 1,616-file final snapshot, 26-file package, nine compiler inputs, eleven artifacts and their preserved original evidence |

The nine changed files and one added regression file were independently reviewed.
The native C sources, profiles, BPF and both ELF binaries are byte-identical to the
initial admission review. Rebuilding binds the updated `contracts.py` source in
`toolchain.json`; that is the only changed native artifact. Production cleanup
methods retain their original implementations.

### Retained test-attempt observations

- The parent's first 112-test remediation attempt encountered a Darwin local
  process-group cleanup refusal during fixture teardown, followed by ResourceWarnings
  when teardown skipped a remaining fixture. Fixture teardown was corrected to
  continue all owned streams and confirm reaping/closed pipes before accepting that
  known refusal. Production cleanup behavior was not weakened. The subsequent
  local 112-test run passed without warnings.
- Correctness's independent full-suite run passed all 112 tests.
- Security's first full-suite run timed out waiting for a fixture's fault-ready
  signal **before the dispatch assertion**. Its unchanged confirmation run passed
  all 112 tests without warnings. Both results are retained; the exact timeout
  cause is unproven. This is a **nonblocking test-reliability follow-up**, not a
  demonstrated admission bypass, and is not relabelled as a passing attempt.
- Independently authored direct no-write probes passed in both review streams.
  Finding closure is supported by those probes and the corrected control flow,
  rather than by discarding an inconvenient full-suite result.

## Exact final bindings

| Record | SHA-256 |
|---|---|
| Final source inventory, 26 files | `f488d5fbfbec00166eccc21c68d38c67aa8905bcd8a828ecf936cc87fb79cf83` |
| Final review intake | `cfff9627e121cca682dee0747bdd72e4a8a04a0261b9d0f03962783b4b150cfb` |
| Local 112-test verification | `fc4aaddc72323336730b436a33f90a7196111e651f2f97287427246219fc4fc6` |
| Rebuilt artifact record | `defa0273f33bb84971c58cefb0b13b60857c0d4c3ecbe3a9c9b81b7c5ff7fdd6` |
| Correctness final review | `0ccb8b68b5df6e3321b6ce2dd60df832493f79671ec2ec77b5f73b08820ea339` |
| Security final review | `b0e773fd252b62ff5aa34ec5858a02fbd3f1f3cc7d155247c4a894803a94c96d` |
| Combined verdicts | `af0ca377acd01f70a4c8c856313be88e7b6155607ac66c162750284b9f965422` |

## Scope and next work

Trusted kernel/clock-context acquisition, the oracle/client-closure worker, full
trial/receipt assembly and exact runtime images remain subsequent implementation
work. Native experiments retain their separate review/approval boundary. Neither
review executed a native actor, Docker operation, tracing experiment or Redis case;
the parent rebuilt artifacts without executing the target or observer.

All primary-worktree unrelated changes and the original design worktree remain
preserved. Source changes and this review checkpoint are local and unstaged. The
[evidence index](evidence/m4-c0-native-admission-review-2026-10-08/README.md) binds ten
exact exports. The [initial implementation report](crawl-jobs-v2-m4-c0-native-admission-2026-10-08.md)
retains its original 99-test, review-pending state; this report records its successor.
