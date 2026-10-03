import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { DatabaseSync } from "node:sqlite";
import { afterEach, describe, expect, it } from "vitest";
import { createCatalogStore } from "../../../catalog-store.ts";
import { ConnectionService } from "../../../connection-service.ts";
import { ProviderLoader } from "../../../providers/provider-loader.ts";
import { AesGcmSecretCodec } from "../../secrets/secret-codec.ts";
import { SqliteRuntimeDatabase } from "./runtime-store.ts";

const directories: string[] = [];
afterEach(async () => {
  for (const path of directories.splice(0)) await rm(path, { recursive: true, force: true });
});
const credential = {
  authType: "oauth2" as const,
  accessToken: "access-fixture",
  refreshToken: "refresh-fixture",
  tokenType: "Bearer",
  expiresAt: new Date(Date.now() + 3600000).toISOString(),
  profile: { accountId: "fixture", displayName: "Fixture", grantedScopes: [] },
  metadata: {},
};
async function fixture() {
  const directory = await mkdtemp(join(tmpdir(), "oc-revoke-"));
  directories.push(directory);
  const path = join(directory, "runtime.sqlite");
  const codec = new AesGcmSecretCodec("old-fixture-key");
  const runtime = new SqliteRuntimeDatabase(path, { secretCodec: codec });
  const raw = new DatabaseSync(path);
  const connection = await runtime.connectionStore.set("example", "default", credential);
  return { runtime, raw, connection, codec, path };
}
describe("persistent non-destructive revocation", () => {
  it.each(["done", "unsupported", "network", "rate_limit"])(
    "records provider outcome %s without destructive fallback or redispatch",
    async (outcome) => {
      const { runtime, raw, connection, codec } = await fixture();
      let calls = 0;
      const service = new ConnectionService({
        catalog: createCatalogStore([]),
        providerLoader: new ProviderLoader({}),
        store: runtime.connectionStore,
        strictRevocationServices: ["example"],
        oauthCredentials: {
          refresh: async (_service, credential) => credential,
          revoke: async () => {
            calls++;
            if (outcome === "network" || outcome === "rate_limit") throw new Error(outcome);
            return outcome === "done" ? "done" : "unsupported";
          },
        },
      });
      try {
        const expected = outcome === "done" ? "REVOKED" : outcome === "unsupported" ? "UNSUPPORTED" : "UNKNOWN";
        expect((await service.revokePreservingCredential(connection.id)).state).toBe(expected);
        expect((await service.revokePreservingCredential(connection.id)).state).toBe(expected);
        expect(calls).toBe(1);
        await expect(service.disconnect("example", undefined, { revoke: true })).rejects.toThrow("non-destructive");
        const value = raw.prepare("select value from connections where id=?").get(connection.id)!.value as string;
        expect(JSON.parse(await codec.decode(value))).toEqual(credential);
      } finally {
        raw.close();
        runtime.close();
      }
    },
  );
  it("retains encrypted credentials and an irreversible barrier after unknown outcomes", async () => {
    const { runtime, raw, connection, codec, path } = await fixture();
    try {
      const safety = runtime.connectionStore.safety;
      const operation = await safety.beginRevocation(connection.id, connection.revision, "example");
      expect(operation.acquired).toBe(true);
      await safety.finishRevocation(operation, "UNKNOWN");
      expect((await safety.beginRevocation(connection.id, connection.revision, "example")).acquired).toBe(false);
      await expect(safety.deleteRevoked(connection.id)).rejects.toThrow();
      await expect(runtime.connectionStore.delete("example", "default")).rejects.toThrow();
      await expect(runtime.connectionStore.set("example", "default", credential)).rejects.toThrow();
      await expect(safety.beginOAuth("example")).rejects.toThrow();
      expect(() =>
        raw.prepare("insert into oauth_states(state,value,created_at) values('legacy','{}',1)").run(),
      ).toThrow();
      await expect(runtime.rotateSecretCodec(new AesGcmSecretCodec("new-fixture-key"))).rejects.toThrow("Reconcile");
      const encrypted = raw.prepare("select value from connections where id=?").get(connection.id)!.value as string;
      expect(encrypted).not.toContain("refresh-fixture");
      expect(JSON.parse(await codec.decode(encrypted))).toEqual(credential);
      const other = new SqliteRuntimeDatabase(path, { secretCodec: codec });
      try {
        expect(await other.connectionStore.get("example", "default")).toMatchObject({ revocationState: "UNKNOWN" });
        await expect(other.connectionStore.safety.beginOAuth("example")).rejects.toThrow();
      } finally {
        other.close();
      }
    } finally {
      raw.close();
      runtime.close();
    }
  });
  it("requires confirmed remote revocation before atomic cleanup and permits a new authorization afterward", async () => {
    const { runtime, raw, connection } = await fixture();
    try {
      const safety = runtime.connectionStore.safety;
      const operation = await safety.beginRevocation(connection.id, connection.revision, "example");
      await safety.finishRevocation(operation, "REVOKED");
      expect(await runtime.connectionStore.get("example", "default")).toBeDefined();
      await expect(runtime.connectionStore.delete("example", "default")).rejects.toThrow();
      await safety.deleteRevoked(connection.id);
      await safety.deleteRevoked(connection.id);
      expect(await runtime.connectionStore.get("example", "default")).toBeUndefined();
      const next = await safety.beginOAuth("example");
      await safety.finishOAuth(next);
    } finally {
      raw.close();
      runtime.close();
    }
  });
  it("blocks shared connections and unfinished OAuth operations, retaining orphan ciphertext across key rotation", async () => {
    const { runtime, raw, connection, codec } = await fixture();
    try {
      const safety = runtime.connectionStore.safety;
      await runtime.connectionStore.set("example", "shared", credential);
      await expect(safety.beginRevocation(connection.id, connection.revision, "example")).rejects.toThrow();
      await runtime.connectionStore.delete("example", "shared");
      const operation = await safety.beginOAuth("example");
      await safety.saveOAuthRecovery(operation, credential);
      await expect(safety.beginRevocation(connection.id, connection.revision, "example")).rejects.toThrow();
      const old = raw.prepare("select value from provider_oauth_operations where id=?").get(operation)!.value as string;
      expect(old).not.toContain("refresh-fixture");
      expect(JSON.parse(await codec.decode(old))).toEqual(credential);
      const next = new AesGcmSecretCodec("next-fixture-key");
      await runtime.rotateSecretCodec(next);
      const rotated = raw.prepare("select value from provider_oauth_operations where id=?").get(operation)!
        .value as string;
      expect(JSON.parse(await next.decode(rotated))).toEqual(credential);
      await expect(codec.decode(rotated)).rejects.toThrow();
    } finally {
      raw.close();
      runtime.close();
    }
  });
});
