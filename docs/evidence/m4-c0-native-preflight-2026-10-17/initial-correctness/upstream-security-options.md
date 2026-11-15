# Public documentation supporting COR-02

Retrieved during the 2026-10-17 review. These are documentation/source excerpts,
not observations of the owner-selected host. No Docker or SSH commands were run.

## Docker Engine API version history

URL: <https://docs.docker.com/reference/api/engine/version-history/#v143-api-changes>

Under **v1.43 API changes**:

> `GET /info` now includes `no-new-privileges` in the `SecurityOptions` string
> list when this option is enabled globally. This change is not versioned, and
> affects all API versions if the daemon has this patch.

## Docker Engine API 1.56 schema

Found through Context7 `/docker/docs`, source:
<https://github.com/docker/docs/blob/main/_vendor/github.com/moby/moby/api/docs/v1.56.yaml>

```yaml
SecurityOptions:
  description: |
    List of security features that are enabled on the daemon, such as
    apparmor, seccomp, SELinux, user-namespaces (userns), rootless and
    no-new-privileges.

    Additional configuration options for each security feature may
    be present, and are included as a comma-separated list of key/value
    pairs.
  type: "array"
  items:
    type: "string"
```

## Moby daemon implementation

URL: <https://raw.githubusercontent.com/moby/moby/master/daemon/info.go>

In `fillSecurityOptions`:

```go
if noNewPrivileges(cfg) {
    securityOptions = append(securityOptions, "name=no-new-privileges")
}
```

URL: <https://raw.githubusercontent.com/moby/moby/master/daemon/info_unix.go>

```go
func noNewPrivileges(cfg *config.Config) bool {
    return cfg.NoNewPrivileges
}
```

The unversioned API-history entry establishes this as supported metadata rather
than an invented test token. The Moby excerpts confirm the exact serialized name;
they are from the published master branch, not an attestation of a particular
installed Docker executable or vendor build.
