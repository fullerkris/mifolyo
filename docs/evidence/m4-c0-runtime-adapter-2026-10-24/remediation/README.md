# R1 / SEC-01 remediation

Both original reviewers identified the same clustered short-option bypass.
Their original failed probes, reports and source snapshot remain untouched.

Reject all unneeded single-dash tokens before binary/config/process side effects.
The only exception is the exact oracle positional suffix:
`--entrypoint python3 sha256:<valid-id> -I -B /oracle/worker.py` at the end of
container creation. No short Docker options are emitted by the typed backend.

Three new test methods cover the clustered/attached forms, malformed or misplaced
oracle suffixes, and real typed oracle-create argv for both architectures using
simulated engine metadata. Lifecycle implementation is unchanged from the first
reviewed snapshot.
