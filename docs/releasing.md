# Releasing a new image

Start to finish: get a token, log in, bump the version, push, point the
clusters at it. If you do this twice a year, step 1 and step 2 are the
ones you will have forgotten.

## Why ghcr.io and not a cluster registry

This is worth understanding before changing any of it, because it looks
like an arbitrary choice and is not.

This webhook is what solves the DNS-01 challenge that issues a cluster's
certificates. That includes the certificate fronting the cluster's own
container registry. If this image lived at `registry.burg.fudo.link`,
then a cluster with no certificate could not pull the image it needs in
order to obtain one: the registry's ingress would be serving a
certificate that does not exist yet.

ghcr.io breaks the cycle. It is public, anonymously pullable, and needs
nothing in the cluster to already be working. For the same reason, the
GHCR package must stay **public** — a private package would need an
`imagePullSecret`, which is one more thing that has to work before
certificates can be issued.

Use the cluster registry for everything else. Not for this.

## 1. Get a GHCR token

**It must be a classic token.** Fine-grained personal access tokens have
no packages permission at all — there is no `write:packages` box to tick
— so they cannot push to GHCR no matter how they are configured. A
fine-grained token fails with a `403` during the bearer-token exchange,
which reads like a scope problem and is not one.

Go to **https://github.com/settings/tokens/new**.

Navigating by hand is where this goes wrong: `Generate new token` is a
dropdown whose *first* entry is the fine-grained one. The classic path is
Settings -> Developer settings -> Personal access tokens -> **Tokens
(classic)** -> Generate new token -> **Generate new token (classic)**.

Tick:

- **`write:packages`** — this is the one. It auto-selects `read:packages`.
- `delete:packages` only if you intend to prune old versions.
- `repo` only if the package is attached to a private repository. This
  one is public, so leave it off.

Tokens expire, so expect to redo this step. You can tell which kind you
ended up with from the token itself: `ghp_` is classic, `github_pat_` is
fine-grained.

> The npm classic-token sunset of late 2025 did **not** apply to GitHub
> personal access tokens. Classic GitHub PATs still exist and are still
> the only thing that authenticates to GHCR.

## 2. Log in

```
skopeo login ghcr.io -u <github-username> --password-stdin
# paste the token, then Ctrl-D
```

`--password-stdin` rather than `--password` keeps the token out of shell
history.

Verify:

```
skopeo login --get-login ghcr.io     # prints your username
```

### Where that credential goes, and why it disappears

By default `skopeo login` writes `$XDG_RUNTIME_DIR/containers/auth.json`
— which is on a **tmpfs**. It does not survive a reboot. That is why
logging in is a step in this runbook rather than a thing you did once.

To keep it, put it somewhere under `$HOME` and point
`REGISTRY_AUTH_FILE` at it **for both the login and the push**:

```
export REGISTRY_AUTH_FILE="$HOME/.config/containers/auth.json"
skopeo login ghcr.io -u <github-username> --password-stdin
```

On NixOS, make that permanent with `home.sessionVariables` (home-manager)
or `environment.sessionVariables` (system-wide). Either only takes effect
in a **new login session**, not the shell you ran the rebuild in.

You do not need `REGISTRY_AUTH_FILE` at all if you are happy with the
default location and re-logging in after a reboot.

One trap: if `docker login` has ever run on this machine with a
credential helper configured, `~/.docker/config.json` holds an *empty*
entry for the registry with the real secret in a keychain skopeo cannot
read. skopeo finds the entry, gets nothing, and pushes anonymously.
`skopeo login` writing a real `auth.json` sidesteps it.

## 3. Bump the version

**`version` in `flake.nix` is the authoritative one.** It names the Go
binary and, via `tags = [ "v${version}" "latest" ]`, the image tag.

```nix
version = "0.1.9";
```

Three other places carry a version string and **none of them are wired to
that one**:

| Where | What it means | Currently |
|---|---|---|
| `flake.nix` `version` | the binary and the image tag | `0.1.8` |
| `Makefile` `IMAGE_TAG` | only the `make build` Docker path | `v1.0.0` |
| `Chart.yaml` `version` | the Helm chart's own version | `1.0.0` |
| `values.yaml` `image.tag` | chart default if a consumer does not override | `v1.0.0` |

They have drifted. The chart's default `image.repository` still points at
`fudoniten/cert-manager-webhook-nexus` on Docker Hub, at a `v1.0.0` tag
that was never published there — so a consumer that does not override
`repository` gets an unpullable image. Both consuming clusters override
enough to work around it. Fixing the default would change which image
Seattle pulls, so it is deliberately left alone here rather than changed
as a side effect.

Bump `Chart.yaml` too if you changed anything under `deploy/`.

## 4. Build and push

```
nix run .#deployContainer
```

This builds the image with `dockerTools.buildLayeredImage` and pushes it
with skopeo to:

```
ghcr.io/fudoniten/cert-manager-webhook-nexus:v<version>
ghcr.io/fudoniten/cert-manager-webhook-nexus:latest
```

**It builds for the build host's architecture only.** If a cluster node
is a different architecture, the pull succeeds and the container dies
with `exec format error`. A multi-arch build needs buildx or a matrix in
CI; this path does not do it.

### First push only: make the package public

A package created by a push is **private**, even when its source
repository is public. Go to the package page -> Package settings ->
Change visibility -> Public.

Skip this and the cluster's anonymous pull fails with something that
looks nothing like a permissions problem from kubelet's side.

## 5. Update the consumers

The clusters pull the chart straight from this repository's `master`, so
chart changes land on their next reconcile. **The image tag does not** —
each cluster pins it:

- `thecitadel-infra`:
  `environments/prod/apps/cert-manager/helmrelease-letsencrypt-nexus-issuer.yaml`
  -> `spec.values.image.tag`
- `seattle-infra`:
  `clusters/production/apps/cert-manager/helmrelease-letsencrypt-nexus-issuer.yaml`
  -> `spec.values.image.tag`

Merging to those repositories' `master` is the deploy.

Note that citadel runs `imagePullPolicy: IfNotPresent` against a pinned
tag deliberately: with `Always`, every pod start re-pulls, so an
unreachable registry turns a restart into a deadlock the cluster cannot
resolve on its own. The cost is that **re-pushing the same tag is not
picked up** — bump `version` here and the tag there together, every time.

## 6. Verify

```
kubectl -n cert-manager get deploy,pod
kubectl -n cert-manager logs deploy/letsencrypt-nexus-issuer-cert-manager-webhook-nexus
```

(The Deployment name is the Helm release name plus the chart name,
because the release name does not contain the chart name.)

Then watch a challenge actually resolve:

```
kubectl get certificate,certificaterequest,order,challenge -A
```

## Troubleshooting a push

**`403 Forbidden` requesting a bearer token.** The credential was found
but may not write where you pointed it. In order of likelihood:

1. The namespace in `repo` is not one your account can push to. On GHCR
   it must be a GitHub user or organisation **login** — `ghcr.io/fudo`
   is a domain name, not an owner, and 403s exactly like a permissions
   failure.
2. The token is fine-grained (see step 1), or is missing `write:packages`.
3. The owner is an organisation with SAML SSO and the token has not been
   authorised for it.

**No credential found.** `deployContainers` checks before it builds and
names the file it consulted. If the message does not appear and the push
fails anyway, the helper is older than that check — see
`fudoniten/fudo-nix-helpers`.

**`exec format error` after a successful pull.** Architecture mismatch;
see step 4.
