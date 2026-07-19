# Per-Host Subnet

Per-Host Subnet maintains host-specific IPv4 routes and selected host-network state from a platform metadata service. The reviewed source and release-candidate artifacts support Linux and Windows builds, but privileged two-host and Windows upgrade/rollback validation is still required before production use.

PastureStack is an independent community effort to preserve, audit, and modernize the Rancher 1.6 ecosystem. It is not affiliated with or endorsed by Rancher Labs or SUSE.

**Upstream:** [`rancher/per-host-subnet`](https://github.com/rancher/per-host-subnet). This GitHub fork retains the upstream Git history, authorship, dates, and license notices unchanged; PastureStack maintenance is consolidated into one commit after the preserved upstream boundary.

The preserved upstream release boundary is `v0.2.4`. Labels `v0.2.5` and `v0.2.6` existed only in a later local maintenance fork and are not represented as upstream releases here. Their reviewed Ubuntu and Go compatibility changes are retained in the PastureStack maintenance commit without inventing an upstream version.

## Current scope

- Reads a minimal host, network, and container model from the configured metadata URL.
- Maintains Linux routes marked with protocol `99`, priority `45160`, the main table, and the local source address.
- Maintains a dedicated Linux `ipset` named `pasturestack-no-host-nat`.
- Maintains Windows routes and host-port mappings created by the current process.
- Registers as the Windows service `pasturestack-per-host-subnet` when explicitly requested.

This repository does not configure a firewall, install network drivers, install or reconfigure Windows Routing and Remote Access, or remove an existing Docker network.

## Safety boundaries

The service refuses ambiguous or conflicting network state. Existing matching Windows routes and port mappings are validated but are not adopted as owned state. After a process restart, stale entries therefore require an operator audit instead of being deleted automatically.

The Windows setup script performs validation only unless `-Apply` is supplied. It requires an explicit adapter and refuses to replace a network or service that has conflicting settings.

## Requirements

- Go 1.26 or newer for local development.
- Linux: `ipset` plus permission to manage routes and the dedicated IP set.
- Windows: Docker with transparent-network support, PowerShell networking cmdlets, and a preconfigured running Routing and Remote Access service.
- A compatible metadata service. Real platform integration and privileged network-namespace tests remain pending.

## Build and test

```sh
go test ./...
go vet ./...
go build -trimpath -buildvcs=false ./
```

The repository also provides `scripts/test`, `scripts/validate`, and `scripts/build` for local validation. A reviewed cross-platform release candidate is built with:

```sh
VERSION_OVERRIDE=v0.2.4 SOURCE_DATE_EPOCH=0 make package
```

This produces deterministic flat assets named `per-host-subnet-0.2.4-linux-amd64.tar.xz` and `per-host-subnet-0.2.4-windows-amd64.zip`. PastureStack Server downloads the Windows asset from its versioned GitHub Release and verifies its SHA-256 digest. Operators do not need to host an artifact mirror. No deployment workflow is included at this stage.

The Windows ZIP retains the internal `rancher/` directory solely for the established Windows agent include/extraction contract. That directory is a compatibility boundary, not current product branding. New executable, service, environment, metadata-label, repository, and external asset names use PastureStack naming.

## Configuration

| Command-line option | Environment variable | Default | Purpose |
| --- | --- | --- | --- |
| `--debug` | `PLATFORM_DEBUG` | `false` | Enable debug-level logs. |
| `--metadata-url` | `PLATFORM_METADATA_URL` | `http://metadata/2016-07-29` | Metadata API base URL. |
| `--metadata-ca-root` | `PLATFORM_CA_ROOT` | empty | Additional PEM CA root for HTTPS metadata. |
| `--metadata-startup-timeout` | `PLATFORM_METADATA_STARTUP_TIMEOUT` | `2m` | Maximum metadata readiness wait. |
| `--watch-interval` | `PLATFORM_WATCH_INTERVAL` | `5s` | Long-poll and retry interval. |
| `--enable-route-update` | `PLATFORM_ENABLE_ROUTE_UPDATE` | `false` | Enable managed host routes. |
| `--route-update-provider` | `PLATFORM_ROUTE_UPDATE_PROVIDER` | `host-gateway` | Route implementation. |
| `--nat-interface` | `PLATFORM_NAT_INTERFACE` | empty | Required Windows interface for host-port mappings. |

Windows-only service actions are available through `--register-service` and `--unregister-service`.

The metadata contract uses these labels:

- `io.pasturestack.network.per-host-subnet.subnet`
- `io.pasturestack.network.per-host-subnet.router-ip`
- `io.pasturestack.network.per-host-subnet.override-agent-ip`

## Windows validation and setup

Run the script first without `-Apply`:

```powershell
.\startup_per-host-subnet.ps1 -AdapterName "Ethernet 2"
```

After reviewing the detected subnet, router address, existing network, executable path, and prerequisites, repeat with `-Apply`. The script creates the named transparent network only when absent, writes service-specific environment variables, registers the service, and starts it.

## Licensing and provenance

The repository remains under the existing Apache License 2.0 terms in [LICENSE](LICENSE). Preserved source history and upstream attribution are documented in [ORIGIN.md](ORIGIN.md). Direct dependency terms are recorded in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md); their license texts are included under `third_party/licenses`.
