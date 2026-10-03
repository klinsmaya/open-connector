# multica-core-v1 contract freeze

Evidence: `sources.json` pins actual repository commits and SHA-256 of
unaltered `.snapshot` files. Fixtures contain synthetic data, not provider
observations. `tests/contract` imports the real Multica SDK through a local
module replacement; it does not reimplement that SDK. Current Multica SDK
and integration source is byte-identical to the research baseline.

| Boundary                     | Contract                                                                                                       | Gateway status  |
| ---------------------------- | -------------------------------------------------------------------------------------------------------------- | --------------- |
| GET toolkits                 | items/next_cursor/total_items; category, limit, cursor, sort_by                                                | SDK_GATEWAY_PG_TESTED |
| GET toolkit                  | stable slug descriptor                                                                                         | SDK_GATEWAY_PG_TESTED |
| GET auth_configs             | single comma-separated toolkit_slug, boolean is_composio_managed, show_disabled                                | SDK_GATEWAY_PG_TESTED |
| POST connected_accounts/link | user_id, auth_config_id, callback_url; redirect_url, expiry, stable account ID                                 | NOT_IMPLEMENTED |
| GET connected_accounts       | repeated user_ids/toolkit_slugs/auth_config_ids/connected_account_ids/statuses; intersection before pagination | NOT_IMPLEMENTED |
| Account response             | id, user_id, auth_config_id, nested auth_config.id, toolkit.slug, uppercase status                             | NOT_IMPLEMENTED |
| POST account/revoke          | unsupported is non-2xx and never 404; preserve credentials on failure                                          | NOT_IMPLEMENTED |
| DELETE account               | SDK treats 404 as success; use 501 for unsupported                                                             | NOT_IMPLEMENTED |
| POST tool_router/session     | user_id, toolkits.enable, connected_accounts map of slug to ID arrays                                          | NOT_IMPLEMENTED |
| MCP response                 | type/http and URL; proposed compat-only headers; current SDK drops headers                                     | NOT_IMPLEMENTED |
| SDK errors                   | nested error with numeric code/status and slug/request_id                                                      | NOT_IMPLEMENTED |
| Native Action                | POST /v1/actions/:actionId, Bearer, x-oo-connector-app-id, Idempotency-Key, input object                       | NOT_TESTED      |
| Native MCP                   | list_apps, list_connections, search_actions, get_action_guide, execute_action                                  | SNAPSHOT_ONLY   |

Compatibility must reject unknown security constraints, empty grants,
unapproved actions and unsupported meta-tools. No default connection or
project-key fallback. Signed cursors must bind the entire authorized query.

The frozen native MCP includes optional connectionName and default selection;
the gateway must narrow that behavior and never forward arbitrary selectors.
The original execute_action schema has no Idempotency-Key. The gateway must
use the native HTTP Action endpoint and own operation identity.

## Integration locations to change after P0

- Startup: Multica `server/cmd/server/router.go`, current Composio block.
- SDK response/auth: `server/pkg/composio/sessions.go`.
- Overlay policy: `server/internal/integrations/composio/dispatch.go`.
- Enqueue: `server/internal/service/task.go` builds and persists overlays.
- Claim: `server/internal/handler/daemon.go` merges RuntimeMcpOverlay; its
  fallback cannot be used in compat mode.
- OAuth ownership/state: integration `service.go` plus authenticated handler.
- Native OAuth owner: OC `src/server/api/connection-routes.ts` originally
  hardcoded local-admin. T05 adds an opt-in native subject namespace. It is
  also mounted for runtime connection creation;
  any subject extension must require actual admin Bearer, never browser auth.
- Native revoke: OC `src/connection-service.ts` currently deletes before
  best-effort remote revoke. A separate operation must retain ciphertext.

SDK fixture success alone does not promote acceptance entries. T03 now has
real SDK/gateway/PostgreSQL coverage for C01/C06; C02/C03/C04 remain partial.
See docs/t03-evidence.md and docs/acceptance.md for scope and limitations.
