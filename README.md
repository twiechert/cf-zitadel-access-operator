# zitadel-access-operator

A Kubernetes operator that protects services with [Cloudflare Access](https://www.cloudflare.com/zero-trust/products/access/) using [Zitadel](https://zitadel.com) OIDC roles. Optionally registers OIDC applications in Zitadel and creates Ingress resources for routing.

No Cloudflare Access Groups needed. Zitadel remains the single source of truth for authorization.

## Prerequisites

- A Zitadel instance with projects and roles configured
- Zitadel configured as an [Identity Provider in Cloudflare Access](https://developers.cloudflare.com/cloudflare-one/identity/idp-integration/generic-oidc/) — users authenticate via Zitadel when accessing any protected domain
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
        │    └─ Creates OIDC application (optional, when spec.oidc is set)
        │
        ├─ Cloudflare (write)
        │    └─ Creates Access Application with OIDC claim policy per role
        │
        └─ Kubernetes (write, optional)
             ├─ Creates Ingress for routing (when spec.tunnel is set)
             └─ Writes OIDC credentials to Secret (when spec.oidc is set)
```

Every `SecuredApplication` creates a Cloudflare Access Application with inline OIDC claim policies that check Zitadel JWT role claims directly. Cloudflare Access handles authentication at the edge — the backend app doesn't need to know about OIDC.

For apps that also do their own OAuth (e.g. Grafana), set `spec.oidc` to register an OIDC application in Zitadel and write the client credentials to a Kubernetes Secret.

## Examples

### Non-OIDC app (most common)

Cloudflare Access protects the app, only users with the right Zitadel roles get through:

```yaml
apiVersion: access.zitadel.com/v1alpha1
kind: SecuredApplication
metadata:
  name: wiki
  namespace: default
spec:
  host: wiki.example.com
  access:
    project: infrastructure
    roles:
      - admin
  tunnel:
    backend:
      serviceName: wiki
      servicePort: 8080
```

### OIDC app (e.g. Grafana)

Same Cloudflare Access protection, plus a Zitadel OIDC app so the backend can do its own OAuth:

```yaml
apiVersion: access.zitadel.com/v1alpha1
kind: SecuredApplication
metadata:
  name: grafana
  namespace: monitoring
spec:
  host: grafana.example.com
  access:
    project: infrastructure
    roles:
      - admin
      - viewer
  oidc:
    redirectURIs:
      - https://grafana.example.com/login/generic_oauth
    idTokenRoleAssertion: true
    accessTokenRoleAssertion: true
  tunnel:
    backend:
      serviceName: grafana
      servicePort: 3000
  deleteProtection: true
```

The operator will:

1. Look up the Zitadel project `infrastructure` and verify the requested roles exist
2. Create a Cloudflare Access Application for the host with a policy that checks the Zitadel JWT role claim directly
3. If `oidc` is set, create a Zitadel OIDC application and write client credentials to a Kubernetes Secret (`{name}-oidc` by default, configurable via `oidc.clientSecretRef`)
4. If `tunnel` is set, create an Ingress (defaults to `ingressClassName: cloudflare-tunnel`, overridable via `tunnel.ingress.className`)

### Delete protection

Set `deleteProtection: true` to keep external resources (Zitadel OIDC app, Cloudflare Access Application) when the CR is deleted. Kubernetes resources (Ingress, Secret) are always cleaned up via owner references.

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
