import type {
  ConnectionSafetyStore,
  StrictRevocationResult,
  StrictRevocationState,
} from "../../connection-revocation.ts";
import type { ResolvedCredential } from "../../core/types.ts";
import type { ISecretCodec } from "../secrets/secret-codec-core.ts";
import type { RequestTransaction } from "./connection-request-store.ts";

import { HttpRequestError } from "../api/http-utils.ts";

export class SqlConnectionSafetyStore implements ConnectionSafetyStore {
  private readonly transaction: RequestTransaction;
  private readonly codec: ISecretCodec;
  constructor(transaction: RequestTransaction, codec: ISecretCodec) {
    this.transaction = transaction;
    this.codec = codec;
  }

  get encrypted(): boolean {
    return this.codec.encrypted;
  }

  async beginOAuth(service: string): Promise<string> {
    const id = crypto.randomUUID();
    const [, , rows] = await this.transaction([
      {
        sql: "insert into provider_revocation_barriers(service) values (?) on conflict(service) do nothing",
        values: [service],
      },
      { sql: "update provider_revocation_barriers set service=service where service=?", values: [service] },
      {
        sql: "insert into provider_oauth_operations(id,service,created_at) select ?,?,? where exists(select 1 from provider_revocation_barriers where service=? and state is null) returning id",
        values: [id, service, new Date().toISOString(), service],
      },
    ]);
    if (!rows.length)
      throw new HttpRequestError(
        "provider_revocation_barrier",
        "Provider authorization is isolated for revocation.",
        409,
      );
    return id;
  }
  async saveOAuthRecovery(id: string, credential: ResolvedCredential): Promise<void> {
    const value = await this.codec.encode(JSON.stringify(credential));
    const [rows] = await this.transaction([
      { sql: "update provider_oauth_operations set value=? where id=? returning id", values: [value, id] },
    ]);
    if (!rows.length)
      throw new HttpRequestError("oauth_recovery_unavailable", "OAuth recovery record is unavailable.", 503);
  }
  async finishOAuth(id: string): Promise<void> {
    await this.transaction([{ sql: "delete from provider_oauth_operations where id=?", values: [id] }]);
  }
  async beginRevocation(
    id: string,
    revision: string,
    service: string,
  ): Promise<StrictRevocationResult & { acquired: boolean }> {
    if (!this.codec.encrypted)
      throw new HttpRequestError("unsupported_revocation", "Encrypted recovery storage is required.", 501);
    const operation = crypto.randomUUID();
    const [, , changed, rows] = await this.transaction([
      {
        sql: "insert into provider_revocation_barriers(service) values (?) on conflict(service) do nothing",
        values: [service],
      },
      { sql: "update provider_revocation_barriers set service=service where service=?", values: [service] },
      {
        sql: `update provider_revocation_barriers set state='REVOKING',connection_id=?,connection_revision=?,operation_id=? where service=? and state is null
       and exists(select 1 from connections where id=? and revision=? and service=? and source='local')
       and not exists(select 1 from connections where service=? and id<>?)
       and not exists(select 1 from provider_oauth_operations where service=?)
       and not exists(select 1 from connection_requests where service=? and phase<>'completed')
       and not exists(select 1 from oauth_states where service=? or service is null)
       and not exists(select 1 from trigger_subscriptions where connection_id=? and status in ('active','deleting')) returning service`,
        values: [id, revision, operation, service, id, revision, service, service, id, service, service, service, id],
      },
      { sql: "select * from provider_revocation_barriers where service=?", values: [service] },
    ]);
    const row = rows[0];
    if (!row?.state || row.connection_id !== id || row.connection_revision !== revision)
      throw new HttpRequestError(
        "revocation_blocked",
        "Revocation is blocked by shared credentials or unfinished provider work.",
        409,
      );
    return {
      connectionId: id,
      operationId: row.operation_id as string,
      state: row.state as StrictRevocationState,
      acquired: changed.length === 1,
    };
  }
  async finishRevocation(result: StrictRevocationResult, state: StrictRevocationState): Promise<void> {
    const [rows] = await this.transaction([
      {
        sql: "update provider_revocation_barriers set state=? where connection_id=? and operation_id=? and state='REVOKING' returning service",
        values: [state, result.connectionId, result.operationId],
      },
    ]);
    if (!rows.length)
      throw new HttpRequestError("revocation_outcome_unknown", "Revocation outcome requires reconciliation.", 503);
  }
  async deleteRevoked(id: string): Promise<void> {
    const [, , , rows] = await this.transaction([
      {
        sql: "update provider_revocation_barriers set state=null where connection_id=? and state='REVOKED'",
        values: [id],
      },
      {
        sql: "delete from connections where id=? and exists(select 1 from provider_revocation_barriers where connection_id=? and state is null)",
        values: [id, id],
      },
      {
        sql: "delete from provider_revocation_barriers where connection_id=? and state is null and not exists(select 1 from connections where id=?)",
        values: [id, id],
      },
      { sql: "select id from connections where id=?", values: [id] },
    ]);
    if (rows.length)
      throw new HttpRequestError(
        "revocation_required",
        "Confirmed remote revocation is required before deleting recovery material.",
        409,
      );
  }
}
