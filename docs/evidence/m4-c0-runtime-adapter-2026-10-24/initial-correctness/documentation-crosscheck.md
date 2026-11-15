# Documentation cross-check — 2026-10-24

Context7 was queried after resolving **Python** to `/python/cpython` and
**Docker CLI** to `/docker/cli`. Its results were current main/master excerpts,
not a promise about an installed CLI. Python's documentation confirms that
`waitid(..., WNOWAIT)` retains waitability and that a negative `Popen.returncode`
records signal termination. These match the local benign-child observations.

For the dispatch finding, the following **Docker CLI v29.0.0** sources were read
and retained as data under `references/`; none was compiled or executed:

- `vendor.mod`: pins Cobra **v1.10.1** and pflag **v1.0.10**. An initial
  `go.mod` HTTP lookup returned 404; the repository intentionally uses
  `vendor.mod` instead. This was a documentation lookup, not a test failure.
- `vendor/github.com/spf13/pflag/flag.go:1040–1128`: consumes multiple boolean
  short flags from one token, and consumes an attached value for string flags.
- `vendor/github.com/spf13/cobra/command.go:914–935,1152–1153,1219–1230`:
  adds default `-h`, parses it, returns `ErrHelp`, and invokes the inherited
  help function. `container inspect` does not redefine `-h`.
- `cmd/docker/docker.go:228–235`: Docker's help function unconditionally calls
  `AddPluginCommandStubs` before displaying help.
- `cli-plugins/manager/cobra.go:18–21`: stub construction calls `ListPlugins`.
- `cli-plugins/manager/manager.go:42–52,125–158`: plugin discovery includes
  default system plugin directories even with an empty private config.
- `cli-plugins/manager/plugin.go:108–109` and
  `cli-plugins/manager/candidate.go:17–18`: discovery calls
  `exec.Command(candidatePath, metadataSubcommand).Output()`.
- `cli/command/cli.go:226–227,285–286,587–595`: the CLI has separate rejection
  of conflicting context/host options and multiple hosts. Therefore the
  adapter accepting `-H<value>`/`-c<value>` is **not** evidence that a request
  actually reached a different daemon.

Each upstream path above is relative to:
<https://github.com/docker/cli/blob/v29.0.0/>.
Eight fetched-file hashes are in `reference-source-hashes.json`. The two small
manager `candidate.go` and `cobra.go` files were additionally retained from the
read-only web-fetch results and are covered by the final artifact manifest.

The exact conclusion is limited to accepted Python-adapter argv plus a pinned
upstream parser/help code path. It is **not** a local Docker/plugin execution
result, a CLI binary admission, or proof about every historical/future version.
