import { randomUUID } from "node:crypto";
import { Pool } from "pg";
import { describe, expect, it } from "vitest";
import { AesGcmSecretCodec } from "../../secrets/secret-codec.ts";
import { migratePostgresDatabase } from "./migrations.ts";
import { PostgresRuntimeDatabase } from "./runtime-store.ts";

const url = process.env.OC_NATIVE_TEST_DATABASE_URL;
describe.skipIf(!url)("real PostgreSQL revocation barriers", () => {
  it("serializes independent instances, guards mutations, and preserves orphan ciphertext", async () => {
    const admin = new Pool({ connectionString: url });
    const schema = `oc_revoke_${randomUUID().replaceAll("-", "")}`;
    await admin.query(`create schema ${schema}`);
    const scoped = new URL(url!);
    scoped.searchParams.set("options", `-c search_path=${schema}`);
    const pool = new Pool({ connectionString: scoped.toString() });
    const codec = new AesGcmSecretCodec("pg-fixture-old-key");
    let first: PostgresRuntimeDatabase | undefined, second: PostgresRuntimeDatabase | undefined;
    try {
      await migratePostgresDatabase({ pool });
      first = await PostgresRuntimeDatabase.open(scoped.toString(), { secretCodec: codec });
      second = await PostgresRuntimeDatabase.open(scoped.toString(), { secretCodec: codec });
      const credential = {
        authType: "oauth2" as const,
        accessToken: "pg-access-fixture",
        refreshToken: "pg-refresh-fixture",
        tokenType: "Bearer",
        profile: { accountId: "fixture", displayName: "Fixture", grantedScopes: [] },
        metadata: {},
      };
      const connection = await first.connectionStore.set("example", "default", credential);
      const a = first.connectionStore.safety!,
        b = second.connectionStore.safety!;
      const pending = await a.beginOAuth("example");
      await a.saveOAuthRecovery(pending, credential);
      await expect(b.beginRevocation(connection.id, connection.revision, "example")).rejects.toThrow();
      const next = new AesGcmSecretCodec("pg-fixture-new-key");
      await first.rotateSecretCodec(next);
      const orphan = (await pool.query("select value from provider_oauth_operations where id=$1", [pending])).rows[0]
        .value;
      expect(JSON.parse(await next.decode(orphan))).toEqual(credential);
      await a.finishOAuth(pending);
      const attempts = await Promise.all([
        a.beginRevocation(connection.id, connection.revision, "example"),
        b.beginRevocation(connection.id, connection.revision, "example"),
      ]);
      expect(attempts.filter((op) => op.acquired)).toHaveLength(1);
      await expect(b.beginOAuth("example")).rejects.toThrow();
      await expect(pool.query("update connections set service='other' where id=$1", [connection.id])).rejects.toThrow(
        "provider_revocation_barrier",
      );
      await expect(
        pool.query("insert into oauth_states(state,value,created_at) values('legacy','{}',1)"),
      ).rejects.toThrow("provider_revocation_barrier");
      await expect(second.connectionStore.delete("example", "default")).rejects.toThrow();
      await a.finishRevocation(attempts.find((op) => op.acquired)!, "UNKNOWN");
      expect((await b.beginRevocation(connection.id, connection.revision, "example")).state).toBe("UNKNOWN");
      await expect(b.deleteRevoked(connection.id)).rejects.toThrow();
      const retained = (await pool.query("select value from connections where id=$1", [connection.id])).rows[0].value;
      expect(JSON.parse(await next.decode(retained))).toEqual(credential);
    } finally {
      await first?.close();
      await second?.close();
      await pool.end();
      await admin.query(`drop schema ${schema} cascade`);
      await admin.end();
    }
  }, 30000);
});
