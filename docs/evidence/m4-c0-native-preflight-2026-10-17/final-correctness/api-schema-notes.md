# API schema verification

Public documentation lookup only; no Docker daemon or owner host was contacted.

Context7 resolved Docker documentation to `/docker/docs`. The current public
Docker Engine **API 1.56** schema was then read directly at:

<https://raw.githubusercontent.com/docker/docs/main/_vendor/github.com/moby/moby/api/docs/v1.56.yaml>

Relevant excerpts:

```yaml
      CpuCfsPeriod:
        description: |
          Indicates if CPU CFS(Completely Fair Scheduler) period is supported by
          the host.
        type: "boolean"
        example: true
      CpuCfsQuota:
        description: |
          Indicates if CPU CFS(Completely Fair Scheduler) quota is supported by
          the host.
        type: "boolean"
        example: true
```

The wire names are `CpuCfsPeriod` and `CpuCfsQuota`, not the former CLI Go-template
names `CPUCfsPeriod` and `CPUCfsQuota`. The nearby `SystemInfo.PidsLimit` is also
a boolean. That must not be confused with the separate container-resource
`PidsLimit` integer elsewhere in the same schema.

The schema defines both `/info` and `/version` as GET operations producing
`application/json`; status 200 refers to `SystemInfo`/`SystemVersion`, and status
500 to `ErrorResponse`. `CgroupVersion` is a string enum `"1"`/`"2"`.
`SystemVersion.KernelVersion` is a string that may be omitted when unavailable;
requiring it for this preflight therefore fails closed on unavailable metadata.

These references validate field mapping and expected response types, not host
identity, actual native feasibility, socket peer trust, or runtime admission.
