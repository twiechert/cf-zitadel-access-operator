# zitadel-access-operator

A Kubernetes operator that bridges [Zitadel](https://zitadel.com) and [Cloudflare Access](https://www.cloudflare.com/zero-trust/products/access/). It lets you declare which Zitadel roles can access a service — the operator creates the Cloudflare Access Application with inline OIDC claim policies and an Ingress for tunnel routing.

No Cloudflare Access Groups needed. Zitadel remains the single source of truth for authorization.

## How it works

```
SecuredApplication CR
        │
        ▼
  zitadel-access-operator
        │
        ├─ Validates project & roles exist in Zitadel (read-only)
        │
        ├─ Creates Cloudflare Access Application
        │    └─ Inline OIDC claim policy per role (no Access Groups)
        │
        └─ Creates Ingress
             └─ Picked up by CF tunnel ingress controller (routing only)
```

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
  tunnel:                      # optional — creates Ingress for CF tunnel routing
    backend:
      serviceName: grafana
      servicePort: 3000
```

The operator will:

1. Look up the Zitadel project `infrastructure` and verify `admin` and `viewer` roles exist
2. Create a Cloudflare Access Application for `grafana.example.com` with an allow policy that checks the Zitadel JWT role claim directly
3. If `tunnel` is set, create an Ingress with `ingressClassName: cloudflare-tunnel` pointing to the backend service

Omit `tunnel` to only create the Access Application (useful when the service is already exposed via another ingress).

Delete the CR and everything is cleaned up (Access Application via finalizer, Ingress via owner reference).

## Installation

### Helm

```bash
helm install zitadel-access-operator ./charts/zitadel-access-operator \
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
helm install zitadel-access-operator ./charts/zitadel-access-operator \
  --set existingSecret=my-credentials \
  --set zitadel.url=https://auth.example.com \
  --set cloudflare.accountId=<CF_ACCOUNT_ID> \
  --set cloudflare.idpId=<CF_IDP_ID>
```

The secret must contain keys `zitadel-token` and `cloudflare-api-token`.

## Configuration

| Flag | Env | Default | Description |
|------|-----|---------|-------------|
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
