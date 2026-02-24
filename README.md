# zitadel-access-operator

A Kubernetes operator that protects services with [Cloudflare Access](https://www.cloudflare.com/zero-trust/products/access/) using [Zitadel](https://zitadel.com) OIDC roles. Declare which Zitadel roles can access a service — the operator configures Cloudflare to enforce it and optionally creates an Ingress for routing.

No Cloudflare Access Groups needed. Zitadel remains the single source of truth for authorization.

## How it works

```
SecuredApplication CR
        │
        ▼
  zitadel-access-operator
        │
        ├─ Zitadel (read-only)
        │    └─ Validates project & roles exist
        │
        ├─ Cloudflare (write)
        │    └─ Creates Access Application with OIDC claim policy per role
        │
        └─ Kubernetes (write, optional)
             └─ Creates Ingress for routing (e.g. via CF tunnel controller)
```

The operator is **read-only on Zitadel** — it never creates or modifies projects, roles, or apps. It only verifies they exist. All write operations go to Cloudflare (Access Application + policy) and optionally Kubernetes (Ingress).

## Custom Resource

```yaml
apiVersion: access.zitadel.com/v1alpha1
kind: SecuredApplication
metadata:
  name: grafana
  namespace: monitoring
spec:
  host: grafana.example.com
  access:
    project: infrastructure   # Zitadel project name (resolved to ID automatically)
    roles:
      - admin
      - viewer
  tunnel:                      # optional — creates an Ingress
    backend:
      serviceName: grafana
      servicePort: 3000
```

The operator will:

1. Look up the Zitadel project `infrastructure` and verify `admin` and `viewer` roles exist
2. Create a Cloudflare Access Application for `grafana.example.com` with a policy that checks the Zitadel JWT role claim directly
3. If `tunnel` is set, create an Ingress (defaults to `ingressClassName: cloudflare-tunnel`, overridable via `tunnel.ingress.className`)

Omit `tunnel` to only configure Cloudflare Access without generating an Ingress.

Delete the CR and everything is cleaned up (Cloudflare Access Application via finalizer, Ingress via owner reference).

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
