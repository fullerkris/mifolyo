# Upstream documentation/source checked independently

Consulted on 2026-10-24. Context7 IDs were independently resolved, then queried:
`/docker/cli` (current index) and `/python/cpython` (current index). Version-specific
Docker statements below use the **v29.0.0** source, not an assumption about a
binary installed on a future host. No Docker executable or plugin was run.

## Clustered help reaches plugin metadata execution

1. [Vendored pflag](https://github.com/docker/cli/blob/v29.0.0/vendor/github.com/spf13/pflag/flag.go),
   package documentation and `parseShortArg` / `parseSingleShortArg`: single-dash
   strings are sequences of shorthand flags; boolean shorthand flags can be
   repeated/combined. `-hh` is two `-h` flags, not an unknown long option.
2. [Root setup](https://github.com/docker/cli/blob/v29.0.0/cli/cobra.go),
   `setupCommonRootCommand`: `rootCmd.PersistentFlags().BoolP("help", "h", false, ...)`.
   Deprecating the shorthand hides/warns; it does not remove it.
3. [Vendored Cobra](https://github.com/docker/cli/blob/v29.0.0/vendor/github.com/spf13/cobra/command.go),
   `execute` (around lines 928–935): help boolean true returns `flag.ErrHelp`,
   before command argument validation. `ExecuteC` (around lines 1148–1154) invokes
   `cmd.HelpFunc()(cmd, args)` even when errors are silenced.
4. [Docker command](https://github.com/docker/cli/blob/v29.0.0/cmd/docker/docker.go),
   `setHelpFunc`: immediately calls
   `pluginmanager.AddPluginCommandStubs(dockerCli, ccmd.Root())`.
5. [Plugin command stubs](https://github.com/docker/cli/blob/v29.0.0/cli-plugins/manager/cobra.go),
   `AddPluginCommandStubs`: calls `ListPlugins`.
6. [Plugin manager](https://github.com/docker/cli/blob/v29.0.0/cli-plugins/manager/manager.go),
   `getPluginDirs`: always appends the platform system directories, even with
   an empty private client configuration.
   [Unix defaults](https://github.com/docker/cli/blob/v29.0.0/cli-plugins/manager/manager_unix.go)
   include `/usr/local/lib/docker/cli-plugins`, `/usr/local/libexec/docker/cli-plugins`,
   `/usr/lib/docker/cli-plugins`, `/usr/libexec/docker/cli-plugins`.
7. [Plugin validation](https://github.com/docker/cli/blob/v29.0.0/cli-plugins/manager/plugin.go),
   `newPlugin` (around lines 108–109), calls `c.Metadata()` for candidates with
   syntactically valid, nonconflicting names.
   [Candidate implementation](https://github.com/docker/cli/blob/v29.0.0/cli-plugins/manager/candidate.go),
   lines 17–18:

   ```go
   func (c *candidate) Metadata() ([]byte, error) {
       return exec.Command(c.path, metadata.MetadataSubcommandName).Output()
   }
   ```

Thus an empty private configuration and disabled hooks do not make a *help*
invocation plugin-free. This is a static v29.0.0 path proof plus a dynamic
adapter-guard bypass proof. It is **not** a claim that a plugin was executed in
this review, that the selected host has a malicious plugin, or that an attacker
can supply arbitrary argv through the current typed backend.

The direct guard also admits some glued `-H...`/`-c...` strings. They are **not**
reported as demonstrated endpoint-override exploits: in this Docker version
global options are parsed before the first command, and placement matters.

## Linux subprocess ownership

[Python OS documentation](https://github.com/python/cpython/blob/main/Doc/library/os.rst):
`WNOWAIT` leaves a child waitable; `WNOHANG` may return `None` while it runs.
[Python subprocess documentation](https://github.com/python/cpython/blob/main/Doc/library/subprocess.rst):
`Popen.poll()` and `wait()` publish `returncode` and can reap the child. The new
transport paths use neither before group signaling. Independent probes on
Python 3.14.8 confirm repeated non-reaping exit observations and signal-before-wait
ordering. Exclusive ownership, Linux waitid semantics, and no external child
reaper remain assumptions; a killed process group is not session/descendant or
remote-container absence proof.

No document/page connector is available in this session; the requested review
deliverables are local Markdown/JSON files in the permitted review directory.
