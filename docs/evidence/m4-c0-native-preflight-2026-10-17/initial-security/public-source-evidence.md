# Docker CLI external evidence (read-only)

Retrieved 2026-10-17 after Context7 resolution `/docker/cli` and two focused documentation queries. References below are Docker CLI **v29.0.0**, not a claimed observation of the installed `moogle-main` client. The reported daemon version 29.9.1 does not pin or attest the client. No Docker command was run in this review.

## Client config is loaded despite explicit `--host`

[`cli/command/cli.go`, lines 211–267](https://github.com/docker/cli/blob/v29.0.0/cli/command/cli.go#L211-L267) applies an optional ConfigDir and unconditionally calls `config.LoadDefaultConfigFile(cli.err)` at line 251. Explicit `--host` resolves the default endpoint but does not omit this load.

[`cli/config/config.go`, lines 73–86, 133–152](https://github.com/docker/cli/blob/v29.0.0/cli/config/config.go) obtains the directory from `DOCKER_CONFIG`, otherwise the user's `.docker` directory, and executes:

```go
filename := filepath.Join(configDir, ConfigFileName)
configFile := configfile.New(filename)
file, err := os.Open(filename)
// ...
err = configFile.LoadFromReader(file)
```

[`cli/config/configfile/file.go`, lines 95–111](https://github.com/docker/cli/blob/v29.0.0/cli/config/configfile/file.go#L95-L111) decodes the complete client config including `auths`/`proxies`; inline registry auth is decoded:

```go
if err := json.NewDecoder(configData).Decode(configFile); err != nil && !errors.Is(err, io.EOF) {
    return err
}
// ...
for addr, ac := range configFile.AuthConfigs {
    if ac.Auth != "" {
        ac.Username, ac.Password, err = decodeAuth(ac.Auth)
        // ...
    }
    // ...
}
```

This establishes conditional credential *reads*, not that registry credentials are printed, transmitted, or that an external credential helper necessarily runs for info/version.

## Projected `info` still executes installed plugin metadata commands

[`cli/command/system/info.go`, lines 85–114](https://github.com/docker/cli/blob/v29.0.0/cli/command/system/info.go#L85-L114):

```go
if plugins, err := pluginmanager.ListPlugins(dockerCli, cmd.Root()); err == nil {
    info.ClientInfo.Plugins = plugins
} else {
    info.ClientErrors = append(info.ClientErrors, err.Error())
}
// Server lookup and output formatting occur afterwards.
return errors.Join(serverConnErr, formatInfo(dockerCli.Out(), info, opts.format))
```

[`cli-plugins/manager/manager.go`, lines 42–51](https://github.com/docker/cli/blob/v29.0.0/cli-plugins/manager/manager.go#L42-L51) searches config-provided extra plugin directories, the client config's `cli-plugins` directory, and system plugin directories. Its `ListPlugins` calls `newPlugin` for each candidate.

[`cli-plugins/manager/plugin.go`, lines 108–119](https://github.com/docker/cli/blob/v29.0.0/cli-plugins/manager/plugin.go#L108-L119) obtains candidate metadata by execution **before** validating its JSON/schema:

```go
meta, err := c.Metadata()
// ...
if err := json.Unmarshal(meta, &p.Metadata); err != nil {
    // ...
}
```

[`cli-plugins/manager/candidate.go`, lines 17–19](https://github.com/docker/cli/blob/v29.0.0/cli-plugins/manager/candidate.go#L17-L19):

```go
func (c *candidate) Metadata() ([]byte, error) {
    return exec.Command(c.path, metadata.MetadataSubcommandName).Output()
}
```

Consequences supported by source: specifying a fixed executable, `--host`, and a projection template does not suppress this program execution; changing to an empty `--config` directory alone still leaves system plugin discovery. Metadata validation happens after plugin code runs. The surrounding Python pipe limit bounds CLI output, not the memory consumed by the Go plugin `.Output()` capture. A malformed plugin need not be accepted for its code to have run.

## Evidence limits

This is upstream source-traced behavior plus a synthetic Popen-environment test. No plugin was installed or executed, no real configuration/credential file was opened by the reviewer, and no claim is made that the owner-selected host actually had such a plugin or inline registry auth. The preflight contains no check that excludes this supported client behavior; therefore its unconditional non-mutation/no-credential-read guarantee is not established.
