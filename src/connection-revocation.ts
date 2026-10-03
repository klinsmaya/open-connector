import type { ResolvedCredential } from "./core/types.ts";

export type StrictRevocationState = "REVOKING" | "REVOKED" | "UNKNOWN" | "UNSUPPORTED";
export interface StrictRevocationResult {
  connectionId: string;
  operationId: string;
  state: StrictRevocationState;
}
/** Durable barriers protect network operations across processes and restarts. */
export interface ConnectionSafetyStore {
  readonly encrypted: boolean;
  beginOAuth(service: string): Promise<string>;
  saveOAuthRecovery(id: string, credential: ResolvedCredential): Promise<void>;
  finishOAuth(id: string): Promise<void>;
  beginRevocation(
    id: string,
    revision: string,
    service: string,
  ): Promise<StrictRevocationResult & { acquired: boolean }>;
  finishRevocation(result: StrictRevocationResult, state: StrictRevocationState): Promise<void>;
  deleteRevoked(id: string): Promise<void>;
}
