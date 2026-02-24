# zitadel-access-operator

A Kubernetes operator that secures services by registering them as OIDC applications in [Zitadel](https://zitadel.com), protecting them with [Cloudflare Access](https://www.cloudflare.com/zero-trust/products/access/) policies, and routing traffic through a Cloudflare Tunnel Ingress.

From a single `SecuredApplication` CR, the operator provisions resources across all three systems.

## Prerequisites

- A Zitadel instance with projects and roles configured
- A [Zitadel Action](https://zitadel.com/docs/apis/actions/code-examples) (`flatRoles`) that maps project roles to the `custom:roles` claim as a flat array — Cloudflare Access can't match Zitadel's default nested role format
- Zitadel configured as an [Identity Provider in Cloudflare Access](https://developers.cloudflare.com/cloudflare-one/identity/idp-integration/generic-oidc/)
- A Cloudflare API token with Access permissions

## How it works

```
SecuredApplication CR
        │
        ▼
  zitadel-access-operator
        │
        ├─ Zitadel
        │    ├─ Validates project & roles exist
        │    └─ Creates OIDC application + writes credentials to K8s Secret
        │
        ├─ Cloudflare
        │    └─ Creates Access Application with policy checking custom:roles claim
        │
        └─ Kubernetes
             ├─ Creates Cloudflare Tunnel Ingress (always)
             └─ Creates direct OIDC Ingress (when nativeOIDC.ingress is set)
```

### Two access paths

Every `SecuredApplication` always gets:
- A **Zitadel OIDC application** (registered for visibility and credential management)
- A **Cloudflare Access Application** with a policy that checks the `custom:roles` JWT claim
- A **Cloudflare Tunnel Ingress** where Cloudflare Access enforces role-based authorization at the edge

Optionally, with `nativeOIDC.ingress`, the operator creates a second Ingress on a different hostname (e.g. `grafana-internal.example.com`) that **bypasses Cloudflare Access entirely**. On this path, the app handles OIDC authentication directly against Zitadel using its own client credentials.

```
External user → grafana.example.com
  → CF Tunnel Ingress → CF Access checks custom:roles → backend

Internal user → grafana-internal.example.com
  → Direct Ingress → app authenticates via Zitadel OIDC natively → backend
```

## Custom Resource

### Basic (CF Access protection only)

```yaml
apiVersion: access.zitadel.com/v1alpha1
kind: SecuredApplication
metadata:
  name: wiki
spec:
  host: wiki.example.com
  access:
    project: infrastructure
    roles: [admin]
  backend:
    serviceName: wiki
    servicePort: 8080
```

The backend doesn't need to know about OIDC — Cloudflare Access handles everything at the edge. The OIDC app is still registered in Zitadel, and credentials are written to the Secret `wiki-oidc`.

### With native OIDC (e.g. Grafana)

```yaml
apiVersion: access.zitadel.com/v1alpha1
kind: SecuredApplication
metadata:
  name: grafana
spec:
  host: grafana.example.com
  access:
    project: infrastructure
    roles: [admin, viewer]
  backend:
    serviceName: grafana
    servicePort: 3000
  nativeOIDC:
    redirectURIs:
      - https://grafana-internal.example.com/login/generic_oauth
    idTokenRoleAssertion: true
    accessTokenRoleAssertion: true
    ingress:
      host: grafana-internal.example.com
      className: nginx
  deleteProtection: true
```

This creates two Ingresses:
- `grafana.example.com` via Cloudflare Tunnel (CF Access enforces roles)
- `grafana-internal.example.com` via nginx (Grafana authenticates users directly against Zitadel)

### Delete protection

Set `deleteProtection: true` to keep external resources (Zitadel OIDC app, Cloudflare Access Application) when the CR is deleted. Kubernetes resources (Ingress, Secret) are always cleaned up via owner references. Defaults to `false`.

## Installation

### Helm

```bash
helm install zitadel-access-operator \
  oci://ghcr.io/twiechert/charts/zitadel-access-operator \
  --namespace zitadel-access-operator \
  --create-namespace \
  --set zitadel.url=https://auth.example.com \
  --set zitadel.token=<ZITADEL_PAT> \
  --set cloudflare.apiToken=<CF_API_TOKEN> \
  --set cloudflare.accountId=<CF_ACCOUNT_ID> \
  --set cloudflare.idpId=<CF_IDP_ID>
```

Or use an existing secret:

```bash
helm install zitadel-access-operator \
  oci://ghcr.io/twiechert/charts/zitadel-access-operator \
  --set existingSecret=my-credentials \
  --set zitadel.url=https://auth.example.com \
  --set cloudflare.accountId=<CF_ACCOUNT_ID> \
  --set cloudflare.idpId=<CF_IDP_ID>
```

The secret must contain keys `zitadel-token` and `cloudflare-api-token`.

## Configuration

| Env | Flag | Default | Description |
|-----|------|---------|-------------|
| `ZITADEL_URL` | `--zitadel-url` | — | Zitadel instance URL |
| `ZITADEL_TOKEN` | — | — | Zitadel PAT (env-only, never in args) |
| `CLOUDFLARE_API_TOKEN` | — | — | Cloudflare API token (env-only, never in args) |
| `CLOUDFLARE_ACCOUNT_ID` | `--cloudflare-account-id` | — | Cloudflare account ID |
| `CLOUDFLARE_IDP_ID` | `--cloudflare-idp-id` | — | CF Access Identity Provider ID for Zitadel |
| — | `--session-duration` | `24h` | CF Access session duration |
| — | `--leader-elect` | `false` | Enable leader election |

## Development

```bash
# Build
just build

# Run tests
just test

# Generate deepcopy & CRD manifests
just generate
just manifests

# Docker
just docker-build
just docker-push
```

## License

MIT
