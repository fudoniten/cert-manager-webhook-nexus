# cert-manager-webhook-nexus

A [cert-manager](https://cert-manager.io/) DNS-01 solver webhook that
answers ACME challenges by writing TXT records through the Nexus DNS API.

cert-manager can solve DNS-01 challenges out of the box for the providers
it ships with. Nexus is not one of them, so it delegates: an ACME
`Issuer` names a `groupName` and `solverName`, cert-manager sends a
`ChallengePayload` to whatever APIService claims that group, and this
webhook is what claims `nexus.fudo.org`. It presents, waits, and cleans
up the record.

## What is in here

| Path | What it is |
|---|---|
| `main.go` | The solver. Implements cert-manager's `webhook.Solver` interface. |
| `deploy/cert-manager-webhook-nexus/` | The Helm chart: Deployment, Service, APIService, RBAC, and the self-signed PKI the APIService needs. |
| `flake.nix` | Nix build of the binary, plus `deployContainer`: builds the image and pushes it to a registry. |
| `Dockerfile` | A plain Docker build of the same binary. Not what the deploy uses. |
| `docs/releasing.md` | **How to build and publish a new image, end to end.** |

## Authentication to Nexus

The solver config accepts exactly one of two credentials, and which one
you use decides which Nexus API version it talks to:

- `apikeysecret` — a shared HMAC key, against the legacy `/api/v2`.
- `privatekeysecret` — this service's Ed25519 private key, as written by
  `nexus-generate-key --keypair`, against the public-key `/api/v3`. Only
  the client holds a private key; the matching public key sits on the
  Nexus server in plaintext, which removes the shared secret the server
  previously had to keep.

Both are `SecretKeySelector`s, so the secret lives in the cluster and is
named rather than inlined.

## Who consumes this

Two clusters install this chart as a Flux `HelmRelease`, fetching the
chart directly from this repository via a `GitRepository`:

- [`thecitadel-infra`](https://github.com/fudoniten/thecitadel-infra) —
  `environments/prod/apps/cert-manager/`
- [`seattle-infra`](https://github.com/fudoniten/seattle-infra) —
  `clusters/production/apps/cert-manager/`

Both override the chart's values rather than editing the chart. See
`docs/releasing.md` § "Update the consumers" for what has to move when a
new image ships.

## Releasing

See **[docs/releasing.md](docs/releasing.md)**. The short version:

```
skopeo login ghcr.io -u <github-username> --password-stdin
# bump `version` in flake.nix
nix run .#deployContainer
# bump image.tag in each consuming HelmRelease
```

## Testing

```
make test               # unit tests
make test-conformance   # cert-manager's ACME conformance suite; needs
                        # envtest binaries and a reachable DNS server
```

The conformance suite is opt-in because it wants `KUBEBUILDER_ASSETS`
(a kube-apiserver and etcd downloaded at test time) plus a live DNS
server at `TEST_DNS_SERVER`.
