# Implementation documentation consulted — 2026-10-24

Context7 resolved Docker CLI `/docker/cli` and Python `/python/cpython` and
returned current plugin-discovery and `waitid(WNOWAIT)` documentation. Docker
behavior was also checked against the upstream **v29.0.0** implementation:

- <https://github.com/docker/cli/blob/v29.0.0/cmd/docker/docker.go>:
  plugin discovery on `info`, help/completion and plugin dispatch; normal
  recognized builtin commands do not enumerate plugin metadata. Hooks require
  a terminal and enabled hooks; this adapter has pipes and disables both hook
  environment switches.
- <https://github.com/docker/cli/blob/v29.0.0/cli/command/cli.go>:
  `--host`, config, context and environment precedence; hooks configuration.
- <https://github.com/docker/cli/blob/v29.0.0/cli/config/config.go> and
  <https://github.com/docker/cli/blob/v29.0.0/cli/config/credentials/default_store.go>:
  absent config uses defaults, including credential-store executable lookup.
  The closed operations do not retrieve credentials or pull images.
- <https://github.com/docker/cli/blob/v29.0.0/cli/command/container/create.go>:
  config-derived proxy injection and the `--pull never` creation path.
- <https://github.com/python/cpython/blob/main/Doc/library/os.rst>:
  `waitid(P_PID, WEXITED | WNOHANG | WNOWAIT)` observes without reaping.

The local machine has no `/usr/bin/docker`. Regression tests use real benign
Python child processes and injected metadata; none execute Docker or a native
C0 actor. A future admitted VM must bind its own Docker binary/version and prove
the full native path. No claim is made that this source inspection admitted a
particular installed Docker binary or every historical/future CLI version.
