# Per-Host Subnet

Per-Host Subnet maintains host-specific IPv4 routes and selected host-network state from a platform metadata service. The reviewed source and release-candidate artifacts support Linux and Windows builds, but privileged two-host and Windows upgrade/rollback validation is still required before production use.

PastureStack is an independent community effort to preserve, audit, and modernize the Rancher 1.6 ecosystem. It is not affiliated with or endorsed by Rancher Labs or SUSE.

**Upstream:** [`rancher/per-host-subnet`](https://github.com/rancher/per-host-subnet). This GitHub fork retains the upstream Git history, authorship, dates, and license notices unchanged. The migration baseline is consolidated immediately after the preserved upstream boundary; later maintenance remains visible as ordinary reviewable commits.

The preserved upstream release boundary is `v0.2.4`. Labels `v0.2.5` and `v0.2.6` existed only in a later local maintenance fork and are not represented as upstream releases here. Their reviewed Ubuntu and Go compatibility changes are retained in the PastureStack maintenance commit without inventing an upstream version.

GitHub retains the pure numeric `v0.2.7` Linux prerelease as immutable review
evidence. `v0.2.8` is the proposed Linux/amd64 rebuild with Go 1.27.0; it is
not an available release until its candidate checks pass and the matching
GitHub asset and SHA-256 are published. PastureStack Server still carries the
separately verified historical Windows compatibility asset version `0.2.4`.

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

- Go 1.27.0, as fixed by the `toolchain` directive and CI.
- Linux: `ipset` plus permission to manage routes and the dedicated IP set.
- Windows: Docker with transparent-network support, PowerShell networking cmdlets, and a preconfigured running Routing and Remote Access service.
- A compatible metadata service. Real platform integration and privileged network-namespace tests remain pending.

## Build and test

```sh
go test ./...
go vet ./...
go build -trimpath -buildvcs=false ./
```

The repository also provides `scripts/test`, `scripts/validate`, and `scripts/build` for local validation. To build the `v0.2.8` candidate from a clean reviewed commit:

```sh
RELEASE_VERSION=v0.2.8 SOURCE_DATE_EPOCH=0 make package
```

This creates `dist/artifacts/per-host-subnet-v0.2.8-linux-amd64` for the overlay
package consumer, the Linux archive, a Windows review ZIP, and `SHA256SUMS` for
the Linux artifacts. Confirm the raw binary's embedded Go version with
`go version -m`, compare it byte-for-byte with the Linux archive entry, and
verify `SHA256SUMS` before publishing. Building the Windows review ZIP does not
approve it for deployment. CI additionally checks the Linux binary's reported
version and runs `govulncheck` on that exact packaged binary.

The historical Server build downloads its Windows compatibility asset from the
matching versioned Server Release and verifies its SHA-256 digest. A future
Windows publication still requires privileged Windows integration. The Linux
binary refresh does not establish privileged two-host, upgrade, or rollback
compatibility, and no deployment workflow is included at this stage.

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
