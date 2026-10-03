# Candidate configuration and deployment differences

These templates describe the local candidate; they do not deploy anything.
No actual production configuration, secret store or migration ledger was read.
Only tracked source/templates and the authorized disposable containers were
inspected. The available runtime is a local gateway scratch image, independently
compiled Multica binary, native source under Node 24 and disposable PostgreSQL 17.

## Observed differences from repository deployment templates

| Tracked template or runtime      | Actual difference requiring review                                                                                                                                                                                    |
| -------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| OC root `docker-compose.yml`     | Uses upstream `oomol-lab/open-connector:latest`, exposes 3000, has no gateway, and does not forward the new trusted-subject/strict-revocation variables. It is not this candidate deployment.                         |
| MC `docker-compose.selfhost.yml` | Uses upstream backend/web tags (default `latest`) and explicit environment entries without Composio compatibility settings. A host `.env` alone does not inject these new entries into the container.                 |
| Gateway executable               | Binds two distinct literal loopback addresses only. Independent Docker service names or `0.0.0.0` are rejected. Use a reviewed co-located proxy/network arrangement; do not expose native/admin/DB ports to an Agent. |
| Gateway scratch image            | Gateway-only, with CA bundle and non-root runtime; native and Multica candidate release images are not built or signed here. Pin fork commits/digests; never substitute an upstream image.                            |
| Multica migration 468            | Two filenames already exist in the original baseline. The actual deployment ledger is unknown. Do not renumber applied history.                                                                                       |
| Provider catalog                 | Only offline `example` is provisioned in fixtures. `approved_providers`, real action IDs and remote-revoke verified lists are empty. A code-supported provider is not an approved grant.                              |

The generic Compose files are deliberately unchanged. Silently enabling trust or
compatibility against upstream latest images would be unsafe and would not solve
loopback/TLS topology requirements. Native capability revision 2 is mandatory;
the gateway rejects an unpatched runtime, including after token issuance/restart.

## Required mapping

`multica-compat.env.example` and `native-compat.env.example` contain names and
non-routable sample origins only. Blank secrets must be supplied privately at
runtime; do not put them into images, repositories, browser URLs or Agent env.
Leave the feature flag false until all mappings and exact read scopes are reviewed.
The strict-revoke provider list remains empty until its grant impact is proven.

| Owner             | Configuration and constraints                                                                                                                                                                                      |
| ----------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Multica only      | `COMPOSIO_API_KEY` is the gateway project control key. `COMPOSIO_BASE_URL` ends in `/api/v3.1`. `COMPOSIO_MCP_ORIGIN` is origin-only. Compatibility explicitly selected; no automatic official fallback.           |
| Multica + gateway | Independent `COMPOSIO_AUTHORITY_TOKEN`, at least 32 characters; gateway reads the same value through `-source-token-file`, not a command-line secret. `-source-origin` identifies the trusted Multica backend.     |
| Gateway only      | `-database-url-file`, `-native-admin-file`, `-vault-key-file` (exactly 32 raw bytes), `-source-token-file` are individually mounted secret files. The database URL and native/admin secrets never enter the Agent. |
| Native only       | Provider client secrets, provider access/refresh tokens and `OOMOL_CONNECT_ENCRYPTION_KEY`; durable encrypted storage. `OOMOL_CONNECT_ADMIN_TOKEN` must match the gateway's admin file.                            |
| Browser/control   | `-public-origin` is the gateway browser/control HTTPS origin; register the exact Multica origin in project `callback_origins`. No wildcards.                                                                       |
| Agent execution   | `-mcp-origin` matches Multica `COMPOSIO_MCP_ORIGIN`; only this listener is reachable by the Agent, using task-scoped bearer.                                                                                       |

For an already provisioned isolated candidate, the gateway command shape is:

```
gateway -database-url-file /run/secrets/gateway-database \
  -control-listen 127.0.0.1:8080 -mcp-listen 127.0.0.1:8081 \
  -public-origin https://gateway-control.example.invalid \
  -mcp-origin https://gateway-mcp.example.invalid \
  -native-url https://native-test.example.invalid \
  -native-admin-file /run/secrets/native-admin \
  -vault-key-file /run/secrets/gateway-vault \
  -source-origin https://multica-test.example.invalid \
  -source-token-file /run/secrets/source-authority
```

The domains above are placeholders, not real endpoints. Fresh gateway databases
start quarantined. Explicit migration/provisioning and reviewed exact project,
subject, auth config, runtime ID and read-action mappings precede serving; the
command does not create a permissive default. Never reopen a restored database
until source state, native credential ownership and blocked recovery are reconciled.

## Callback chain

1. Provider developer application registers the native runtime's configured
   OAuth callback, normally its HTTPS `/oauth/callback` endpoint; inspect that
   provider's definition for any provider-specific callback variant.
2. Native connection request returns the browser to gateway `/connect/{id}/poll`
   with the gateway nonce. Browser query status is never proof of success.
3. Gateway returns a short verifier ticket to Multica
   `/api/integrations/composio/verify`. The browser must have a valid human JWT
   login cookie for the expected subject; bearer JWT/PAT/headers do not replace it.
4. Multica's control channel verifies the ticket and preserves the signed state
   for `/api/integrations/composio/callback`. Its origin/path must match configured
   `COMPOSIO_CALLBACK_BASE_URL`. No arbitrary return origin is accepted.

Local HTTP is allowed only with explicit loopback configuration. A real provider
may impose HTTPS, developer-app allowlists or localhost exceptions. Those are
provider-specific prerequisites, not reasons to weaken gateway validation.

## Minimal unresolved external inputs

No real provider/service was selected in the authorized manifest. The plan's
Google grant warning is not a selection of Google. The next live test requires:

- One named provider and exact read-only action(s); a dedicated test account with
  self-owned test data, its developer OAuth app/client, requested scopes, and
  evidence of grant exclusivity/revocation blast radius. Supply credential
  references through the chosen runtime secret mechanism, never in chat.
- Native, gateway-control, gateway-MCP and Multica test origins, browser login
  and the provider-accepted native callback registration. No real origin is
  present in this checkout.
- For model-mediated M03 only: chosen model/provider account and explicit call
  authorization. Codex `0.159.0-alpha.3` protocol is verified offline; other CLI
  versions must be selected explicitly before claiming their compatibility.
- For deployment U02 only: target fork commits/images, topology/TLS enforcement,
  actual migration ledger (including both 468 filenames), signing destination
  and separate staging/deployment authorization. There is no authorized target
  version beyond the local candidate commits.

Use the isolated canary reproduction for local verification. Official rollback
without approved official credentials is compatibility disable, not a reconnect
or credential transfer. A real official reconnect requires its separately
provisioned project and user consent.
