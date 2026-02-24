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
             └─ Creates Ingress for Cloudflare Tunnel routing
```

When a user hits the protected domain, Cloudflare Access redirects them to Zitadel for authentication. The resulting JWT contains the `custom:roles` claim (a flat array of role names). The Access policy checks this claim against the allowed roles. The backend receives the authenticated request via the Cloudflare Tunnel.

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
    project: infrastructure   # Zitadel project name (resolved to ID)
    roles:
      - admin
      - viewer
  backend:
    serviceName: grafana
    servicePort: 3000
  oidc:                        # optional overrides for the Zitadel OIDC app
    redirectURIs:
      - https://grafana.example.com/login/generic_oauth
    idTokenRoleAssertion: true
    accessTokenRoleAssertion: true
  deleteProtection: true       # keep external resources on CR deletion
```

The operator will:

1. Look up the Zitadel project `infrastructure` and verify `admin` and `viewer` roles exist
2. Create a Zitadel OIDC application and write client credentials to a Kubernetes Secret (`grafana-oidc` by default, configurable via `oidc.clientSecretRef`)
3. Create a Cloudflare Access Application for `grafana.example.com` with a policy that checks the `custom:roles` claim
4. Create an Ingress with `ingressClassName: cloudflare-tunnel` (overridable via `ingress.className`)

The `oidc` section is optional — omit it to use sensible defaults. The OIDC application is always created in Zitadel regardless.

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
