// Real OpenConnector HTTP runtime with an offline OAuth exchange fixture.
import { serve } from "@hono/node-server";
import { createCatalogStore } from "../../../src/catalog-store.ts";
import { ProviderLoader } from "../../../src/providers/provider-loader.ts";
import { createConnectApp } from "../../../src/server/connect-app.ts";
import { TransitFileService } from "../../../src/server/files/transit-files.ts";
import { PlainTextSecretCodec } from "../../../src/server/secrets/secret-codec-core.ts";
import { SqliteRuntimeDatabase } from "../../../src/server/storage/sqlite/runtime-store.ts";

const database = new SqliteRuntimeDatabase(":memory:");
await database.oauthClientConfigStore.set({
  service: "example",
  clientId: "fixture",
  clientSecret: "fixture-secret",
  extra: {},
  secretExtra: {},
});
let dispatch: (request: Request) => Response | Promise<Response> = () => new Response(null, { status: 503 });
const server = serve({ hostname: "127.0.0.1", port: 0, fetch: (request) => dispatch(request) });
await new Promise<void>((resolve) => server.on("listening", resolve));
const address = server.address();
if (!address || typeof address === "string") throw new Error("Missing loopback listener");
const origin = `http://127.0.0.1:${address.port}`;
const { app } = await createConnectApp({
  catalog: createCatalogStore([
    {
      service: "example",
      displayName: "Example",
      categories: [],
      authTypes: ["oauth2"],
      auth: [
        {
          type: "oauth2",
          authorizationUrl: "https://provider.invalid/authorize",
          tokenUrl: "https://provider.invalid/token",
          scopes: ["read"],
          tokenEndpointAuthMethod: "client_secret_post",
        },
      ],
      actions: [],
    },
  ]),
  runtimeDatabase: database,
  providerLoader: new ProviderLoader({
    example: async () => ({
      executors: {},
      oauth: {
        exchangeCode: async () => ({
          accessToken: "fixture-provider-secret",
          tokenType: "Bearer",
          metadata: { scope: "read" },
        }),
      },
    }),
  }),
  transitFiles: new TransitFileService({
    rootDir: ".tmp/native-flow",
    publicOrigin: origin,
    ttlSeconds: 60,
    maxBytes: 1024,
  }),
  publicOrigin: origin,
  secretCodec: new PlainTextSecretCodec(),
  adminToken: "native-admin-fixture",
  trustedSubjectRequests: true,
});
dispatch = app.fetch;
console.log(origin);
process.on("SIGTERM", () => {
  server.close();
  database.close();
  process.exit(0);
});
