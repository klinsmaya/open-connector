import type { ConnectionService } from "../../connection-service.ts";
import type { OAuthFlowService } from "../../oauth/oauth-flow-service.ts";
import type { SaasOAuthService } from "../../saas/saas-oauth-service.ts";
import type { Context } from "hono";
import type { z } from "zod";

import { Hono } from "hono";
import { ConnectionError } from "../../connection-service.ts";
import { OAuthClientConfigError } from "../../oauth/oauth-client-config-service.ts";
import { OAuthFlowError } from "../../oauth/oauth-flow-service.ts";
import { SaasError } from "../../saas/saas-client.ts";
import { hasAdminBearer } from "./auth.ts";
import { readJsonBody, HttpRequestError } from "./http-utils.ts";
import {
  connectionManagementFailure,
  serializeManagedConnection,
  writeRuntimeSuccess,
  writeRuntimeFailure,
} from "./runtime-api.ts";

interface ConnectionRoutesOptions {
  trustedSubjectRequests?: boolean;
  connections: ConnectionService;
  oauthFlow: OAuthFlowService;
  saasOAuth?: SaasOAuthService;
}

/** Personal connection management. Authentication runs in the parent app. */
export function createConnectionRoutes({
  connections,
  oauthFlow,
  saasOAuth,
  trustedSubjectRequests,
}: ConnectionRoutesOptions): Hono {
  const app = new Hono();
  // This is only an OAuth transaction namespace; connection ownership remains
  // the trusted gateway's responsibility and execution still needs exact grants.
  const owner = (context: Context): string => {
    const subject = context.req.header("X-Connector-Subject");
    return trustedSubjectRequests && subject !== undefined ? `trusted-subject:${subject}` : "local-admin";
  };
  app.use("*", async (context, next) => {
    const subject = context.req.header("X-Connector-Subject");
    if (subject !== undefined) {
      if (!trustedSubjectRequests)
        return writeRuntimeFailure(context, {
          status: 400,
          errorCode: "unsupported_subject_context",
          message: "Subject-scoped requests are not enabled.",
        });
      if (!hasAdminBearer(context))
        return writeRuntimeFailure(context, {
          status: 403,
          errorCode: "forbidden",
          message: "Subject context requires an administrator bearer.",
        });
      if (!/^[A-Za-z0-9_.:-]{1,128}$/.test(subject))
        return writeRuntimeFailure(context, {
          status: 400,
          errorCode: "invalid_subject",
          message: "Invalid subject context.",
        });
    }
    await next();
  });
  app.onError((error, context) => {
    if (error instanceof SaasError) {
      if (error.retryAfter) context.header("Retry-After", error.retryAfter);
      return writeRuntimeFailure(context, {
        status: error.status,
        errorCode: error.code,
        message: error.message,
        data: error.connectionRequestId ? { connectionRequestId: error.connectionRequestId } : undefined,
      });
    }
    if (
      error instanceof ConnectionError ||
      error instanceof OAuthFlowError ||
      error instanceof OAuthClientConfigError
    ) {
      return writeRuntimeFailure(context, connectionManagementFailure(error));
    }
    throw error;
  });
  app.get("/connections", async (context) => {
    const { connectionStatusInput } = await import("./connection-input.ts");
    const status = parseBody(connectionStatusInput, context.req.query("status"));
    const apps = (await connections.listManagedConnections()).map(serializeManagedConnection);
    return writeRuntimeSuccess(context, status ? apps.filter((app) => app.status === status) : apps);
  });
  app.get("/connections/by-id/:appId", async (context) => {
    return writeRuntimeSuccess(
      context,
      serializeManagedConnection(await connections.getManagedConnection(context.req.param("appId"))),
    );
  });
  app.get("/connection-requests/:connectionRequestId", async (context) => {
    const id = context.req.param("connectionRequestId");
    const request =
      saasOAuth && hasAdminBearer(context)
        ? await saasOAuth.sync(id, owner(context), context.req.raw.signal)
        : await oauthFlow.getConnectionRequest(id, owner(context));
    if (!request)
      return writeRuntimeFailure(context, {
        status: 404,
        errorCode: "connection_request_not_found",
        message: "Connection request not found.",
      });
    return writeRuntimeSuccess(context, request);
  });
  for (const reconnect of [false, true]) {
    const path = reconnect ? "/connections/by-id/:appId/connect" : "/connections/:service/connect";
    app.post(path, async (context) => {
      const { oauthConnectionInput } = await import("./connection-input.ts");
      const input = parseBody(oauthConnectionInput, await readJsonBody(context));
      const target = reconnect ? await connections.getStoredConnection(context.req.param("appId")!) : undefined;
      return writeRuntimeSuccess(
        context,
        await oauthFlow.startConnectionRequest({
          ...input,
          service: target?.service ?? context.req.param("service")!,
          owner: owner(context),
          signal: context.req.raw.signal,
          target,
        }),
      );
    });
    for (const authType of ["api-key", "custom-credential"]) {
      app.post(`${path}/${authType}`, async (context) => {
        const { apiKeyConnectionInput, customConnectionInput } = await import("./connection-input.ts");
        const body = await readJsonBody(context);
        const target = reconnect ? await connections.getStoredConnection(context.req.param("appId")!) : undefined;
        if (
          target &&
          (target.source === "saas" ||
            target.credential.authType !== (authType === "api-key" ? "api_key" : "custom_credential"))
        ) {
          throw new ConnectionError("unsupported_auth_type", "The connection uses a different credential type.");
        }
        const service = target?.service ?? context.req.param("service")!;
        const options = {
          connectionName: target?.connectionName ?? crypto.randomUUID(),
          expectedConnection: target,
          signal: context.req.raw.signal,
        };
        let summary;
        if (authType === "api-key") {
          const input = parseBody(apiKeyConnectionInput, body);
          summary = await connections.connectWithApiKey(service, {
            ...options,
            values: { ...input.extra, apiKey: input.apiKey },
            comment: input.comment,
          });
        } else {
          const input = parseBody(customConnectionInput, body);
          summary = await connections.connectWithCustomCredential(service, {
            ...options,
            values: input.values,
            comment: input.comment,
          });
        }
        return writeRuntimeSuccess(
          context,
          serializeManagedConnection(await connections.getManagedConnection(summary.id)),
        );
      });
    }
  }
  return app;
}

function parseBody<T>(schema: z.ZodType<T>, body: unknown): T {
  const parsed = schema.safeParse(body);
  if (!parsed.success)
    throw new HttpRequestError(
      "invalid_input",
      parsed.error.issues.map((issue) => `${issue.path.join(".")}: ${issue.message}`).join("; "),
    );
  return parsed.data;
}
