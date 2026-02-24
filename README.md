# zitadel-access-operator

A Kubernetes operator that registers OIDC applications in [Zitadel](https://zitadel.com), protects them with [Cloudflare Access](https://www.cloudflare.com/zero-trust/products/access/) policies based on Zitadel roles, and optionally creates an Ingress for routing.

No Cloudflare Access Groups needed. Zitadel remains the single source of truth for authorization.

## How it works

```
SecuredApplication CR
        │
        ▼
  zitadel-access-operator
        │
        ├─ Zitadel (write)
        │    ├─ Validates project & roles exist
        │    ├─ Creates OIDC application
        │    └─ Writes client credentials to K8s Secret
        │
        ├─ Cloudflare (write)
        │    └─ Creates Access Application with OIDC claim policy per role
        │
        └─ Kubernetes (write, optional)
             └─ Creates Ingress for routing (e.g. via CF tunnel controller)
```

From a single CR the operator provisions resources across three systems: a Zitadel OIDC application, a Cloudflare Access Application with inline OIDC claim policies (checking Zitadel JWT role claims directly), and optionally a Kubernetes Ingress.

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
  oidc:                        # optional — OIDC app settings
    redirectURIs:
      - https://grafana.example.com/login/generic_oauth
    idTokenRoleAssertion: true
    accessTokenRoleAssertion: true
  tunnel:                      # optional — creates an Ingress
    backend:
      serviceName: grafana
      servicePort: 3000
```

The operator will:

1. Look up the Zitadel project `infrastructure` and verify `admin` and `viewer` roles exist
2. Create a Zitadel OIDC application and write the client credentials to a Kubernetes Secret (`grafana-oidc` by default, configurable via `oidc.clientSecretRef`)
3. Create a Cloudflare Access Application for `grafana.example.com` with a policy that checks the Zitadel JWT role claim directly
4. If `tunnel` is set, create an Ingress (defaults to `ingressClassName: cloudflare-tunnel`, overridable via `tunnel.ingress.className`)

Omit `tunnel` to skip Ingress creation. Omit `oidc` to use sensible defaults (redirect to `https://{host}/callback`, authorization code flow, basic auth).

Delete the CR and everything is cleaned up (Zitadel OIDC app + Cloudflare Access Application via finalizer, Ingress + Secret via owner reference).

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
