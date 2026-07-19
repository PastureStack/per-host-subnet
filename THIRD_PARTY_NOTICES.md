# Third-party notices

This file records direct and transitive modules included by the current Go module graph. It does not replace the corresponding license texts.

| Module | Version | License material |
| --- | --- | --- |
| `github.com/vishvananda/netlink` | `v1.3.1` | Apache License 2.0; `third_party/licenses/netlink-LICENSE` |
| `github.com/vishvananda/netns` | `v0.0.5` | Apache License 2.0; `third_party/licenses/netns-LICENSE` |
| `golang.org/x/sys` | `v0.47.0` | BSD-style license and additional patent grant; `third_party/licenses/x-sys-LICENSE` and `third_party/licenses/x-sys-PATENTS` |

Dependency versions are locked by `go.mod` and `go.sum`. Run `go mod verify` before a release review.
