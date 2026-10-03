// Real OpenConnector HTTP runtime with an offline OAuth exchange fixture.
import { serve } from "@hono/node-server";
import { DatabaseSync } from "node:sqlite";
import { createCatalogStore } from "../../../src/catalog-store.ts";
import { s } from "../../../src/core/json-schema.ts";
import { defineProviderAction } from "../../../src/core/provider-definition.ts";
import { ProviderLoader } from "../../../src/providers/provider-loader.ts";
import { createConnectApp } from "../../../src/server/connect-app.ts";
import { TransitFileService } from "../../../src/server/files/transit-files.ts";
import { AesGcmSecretCodec } from "../../../src/server/secrets/secret-codec.ts";
import { SqliteRuntimeDatabase } from "../../../src/server/storage/sqlite/runtime-store.ts";

let executions = 0;
const codec = new AesGcmSecretCodec("native-encryption-fixture");
const fixturePath = process.env.OC_NATIVE_FIXTURE_DB;
const database = new SqliteRuntimeDatabase(fixturePath ?? ":memory:", { secretCodec: codec });
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
  catalog: createCatalogStore(
    [
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
        actions: [
          defineProviderAction("example", {
            name: "read",
            description: "Read fixture",
            operationType: "read",
            inputSchema: s.object({ value: s.string() }),
            outputSchema: s.object({ value: s.string(), executions: s.number() }),
          }),
        ],
      },
    ],
    { executableActionIds: ["example.read"] },
  ),
  runtimeDatabase: database,
  providerLoader: new ProviderLoader({
    example: async () => ({
      executors: {
        "example.read": async (input) => ({ ok: true, output: { ...(input as object), executions: ++executions } }),
      },
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
  secretCodec: codec,
  adminToken: "native-admin-fixture",
  trustedSubjectRequests: true,
});
if (fixturePath)
  app.get("/__fixture/recovery", async (context) => {
    if (context.req.header("Authorization") !== "Bearer native-admin-fixture")
      return context.json({ error: "forbidden" }, 403);
    const raw = new DatabaseSync(fixturePath, { readOnly: true });
    try {
      const rows = raw.prepare("select value from connections where source='local'").all();
      let retained = false;
      for (const row of rows) {
        const encrypted = String(row.value);
        const value = JSON.parse(await codec.decode(encrypted));
        if (value.accessToken === "fixture-provider-secret" && !encrypted.includes("fixture-provider-secret"))
          retained = true;
      }
      return context.json({ encryptedOrphanRetained: retained });
    } finally {
      raw.close();
    }
  });
dispatch = async (request) => {
  let loseResponse = false;
  if (request.method === "POST" && new URL(request.url).pathname === "/v1/actions/example.read") {
    const body = (await request.clone().json()) as { input?: { value?: string } };
    loseResponse = body.input?.value === "__lose_response__";
  }
  const response = await app.fetch(request);
  return loseResponse ? new Response(null, { status: 503 }) : response;
};
console.log(origin);
process.on("SIGTERM", () => {
  server.close();
  database.close();
  process.exit(0);
});
