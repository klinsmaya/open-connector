// Explicit disposable test launcher. No provider exchange or model turn occurs.
// The container has no IP egress; the Unix socket reaches only the local test PG.
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { readFile, writeFile, copyFile, rm } from "node:fs/promises";
import { createServer, connect } from "node:net";
import { createInterface } from "node:readline";
import { DatabaseSync, backup } from "node:sqlite";
import pg from "pg";

const fixture = JSON.parse(await readFile("/fixture/canary.json", "utf8"));
const children = new Set();
const bridge = createServer((client) => {
  const upstream = connect("/fixture/postgres.sock");
  client.pipe(upstream).pipe(client);
  client.on("error", () => upstream.destroy());
  upstream.on("error", () => client.destroy());
});
await new Promise((resolve) => bridge.listen(55432, "127.0.0.1", resolve));
const database = new pg.Client({ connectionString: fixture.database });
await database.connect();
const forbidden = new pg.Client({
  connectionString: fixture.database.replace(/\/multica_canary_[^?]+/, "/multica_recovery_test"),
});
await assert.rejects(forbidden.connect(), /does not exist/);
await forbidden.end();
assert.match(fixture.schema, /^gateway_claim_\d+$/);
await database.query(`SET search_path TO ${fixture.schema}`);
const gatewayURL = new URL(fixture.database);
gatewayURL.searchParams.set("search_path", fixture.schema);
await writeFile("/tmp/database", gatewayURL.toString(), { mode: 0o600 });
await writeFile("/tmp/native", "native-admin-fixture", { mode: 0o600 });
await writeFile("/tmp/vault", Buffer.alloc(32, 7), { mode: 0o600 });
await writeFile("/tmp/source", "a".repeat(32), { mode: 0o600 });
await copyFile("/fixture/native.sqlite", "/tmp/native.sqlite");
const start = (binary, args, env = {}) => {
  const child = spawn(binary, args, {
    cwd: "/tmp",
    env: { PATH: "/nonexistent", ...env },
    stdio: ["ignore", "pipe", "pipe"],
  });
  children.add(child);
  // Bounded diagnostics are withheld unless startup fails; fixture secrets are
  // never logged. Output is drained so background logging cannot block a child.
  child.diagnostic = "";
  child.stderr.on("data", (chunk) => {
    child.diagnostic = (child.diagnostic + chunk).slice(-4000);
  });
  return child;
};
const stop = async (child) => {
  if (child.exitCode === null) {
    const exited = new Promise((resolve) => child.once("exit", resolve));
    child.kill("SIGKILL");
    await exited;
  }
  children.delete(child);
};
const finish = (child) =>
  new Promise((resolve, reject) => {
    child.on("error", reject);
    child.once("exit", (code) => {
      children.delete(child);
      resolve(code);
    });
    child.stdout.resume();
  });
const waitHTTP = async (url) => {
  for (let i = 0; i < 120; i++) {
    try {
      const r = await fetch(url, { signal: AbortSignal.timeout(500) });
      await r.arrayBuffer();
      if (r.status === 200) return;
    } catch {}
    await new Promise((resolve) => setTimeout(resolve, 50));
  }
  throw new Error(`local listener unavailable: ${url}; ${gateway?.diagnostic ?? multica?.diagnostic ?? ""}`);
};
const mcEnv = {
  DATABASE_URL: fixture.database,
  JWT_SECRET: "isolated-canary-jwt-fixture-32bytes",
  PORT: "8030",
  APP_ENV: "development",
  COMPOSIO_BACKEND: "compat",
  COMPOSIO_BASE_URL: "http://127.0.0.1:8080/api/v3.1",
  COMPOSIO_API_KEY: "control-fixture",
  COMPOSIO_MCP_ORIGIN: "http://127.0.0.1:8081",
  COMPOSIO_ALLOW_LOOPBACK: "true",
  COMPOSIO_AUTHORITY_TOKEN: "a".repeat(32),
  COMPOSIO_STATE_SECRET: "state-fixture",
  COMPOSIO_CALLBACK_BASE_URL: "http://127.0.0.1:8030",
  FF_COMPOSIO_MCP_APPS: "true",
};
let native, multica, gateway;
const request = async (url, method, headers, body) => {
  const response = await fetch(url, {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
    signal: AbortSignal.timeout(6000),
    redirect: "error",
  });
  return { status: response.status, body: await response.text() };
};
const mcp = (endpoint, bearer, method, params) =>
  request(
    endpoint,
    "POST",
    {
      Authorization: `Bearer ${bearer}`,
      "Content-Type": "application/json",
      Accept: "application/json, text/event-stream",
    },
    { jsonrpc: "2.0", id: 1, method, params },
  );
try {
  const before = (await database.query("SELECT runtime_ciphertext FROM session WHERE state='ACTIVE'")).rows;
  assert.equal(before.length, 1);
  assert.ok(before[0].runtime_ciphertext.length > 0);
  assert.equal(
    await finish(start("/gateway", ["-mode", "quarantine-restore", "-database-url-file", "/tmp/database"])),
    0,
  );
  assert.notEqual(await finish(start("/gateway", ["-database-url-file", "/tmp/database"])), 0);
  assert.equal((await database.query("SELECT count(*)::int AS n FROM session WHERE state='ACTIVE'")).rows[0].n, 0);
  assert.deepEqual(
    (await database.query("SELECT runtime_ciphertext FROM session")).rows[0].runtime_ciphertext,
    before[0].runtime_ciphertext,
  );
  console.log(
    "PASS coordinated PG/native snapshot restore: listeners closed; old sessions quarantined; runtime ciphertext retained",
  );

  const startNative = async () => {
    native = start("/node", ["/oc/gateway/tests/integration/native_runtime.ts"], {
      OC_NATIVE_FIXTURE_DB: "/tmp/native.sqlite",
    });
    const lines = createInterface({ input: native.stdout });
    const origin = await new Promise((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error("native startup timeout: " + native.diagnostic)), 6000);
      lines.once("line", (line) => {
        clearTimeout(timer);
        resolve(line);
      });
    });
    assert.match(origin, /^http:\/\/127\.0\.0\.1:\d+$/);
    return origin;
  };
  let nativeOrigin = await startNative();
  const recovery = async () =>
    JSON.parse(
      (await request(nativeOrigin + "/__fixture/recovery", "GET", { Authorization: "Bearer native-admin-fixture" }))
        .body,
    );
  assert.equal((await recovery()).encryptedCredentialRetained, true);
  const nativeHeaders = {
    Authorization: "Bearer native-admin-fixture",
    "X-Connector-Subject": "independent-orphan-fixture",
    "Content-Type": "application/json",
  };
  const started = JSON.parse(
    (
      await request(nativeOrigin + "/v1/connections/example/connect", "POST", nativeHeaders, {
        returnUri: "http://127.0.0.1:1/complete",
      })
    ).body,
  ).data;
  const state = new URL(started.authorizationUrl).searchParams.get("state");
  const callback = await fetch(
    nativeOrigin + "/oauth/callback?code=orphan-fixture&state=" + encodeURIComponent(state),
    { headers: nativeHeaders, redirect: "manual" },
  );
  assert.equal(callback.status, 302);
  await callback.arrayBuffer();
  const orphanID = JSON.parse(
    (await request(nativeOrigin + "/v1/connection-requests/" + started.connectionRequestId, "GET", nativeHeaders)).body,
  ).data.appId;
  const orphan = (await recovery()).connections.find((row) => row.id === orphanID);
  assert.ok(orphan?.ciphertextSHA256);
  assert.equal(
    (await database.query("SELECT count(*)::int AS n FROM connection WHERE native_id=$1", [orphanID])).rows[0].n,
    0,
  );
  const sqlite = new DatabaseSync("/tmp/native.sqlite", { readOnly: true });
  await backup(sqlite, "/tmp/native-restored.sqlite");
  sqlite.close();
  await stop(native);
  await rm("/tmp/native.sqlite-wal", { force: true });
  await rm("/tmp/native.sqlite-shm", { force: true });
  await copyFile("/tmp/native-restored.sqlite", "/tmp/native.sqlite");
  nativeOrigin = await startNative();
  assert.deepEqual(
    (await recovery()).connections.find((row) => row.id === orphanID),
    orphan,
  );
  console.log(
    "PASS independent unmapped OAuth orphan survives native backup/restart by exact ID and ciphertext digest",
  );
  // Explicit fixture reconciliation after verifying source identities, mapping and
  // preserved credential. Old sessions stay QUARANTINED; no broad reopen SQL.
  assert.equal(
    (
      await database.query(
        "SELECT count(*)::int AS n FROM public.composio_compat_task WHERE task_id=$1 AND agent_id=$2",
        [fixture.task, fixture.agent],
      )
    ).rows[0].n,
    1,
  );
  await database.query(
    "UPDATE connection SET state='ACTIVE' WHERE project_id='p' AND id='ca_fixture' AND subject_id=$1 AND state='QUARANTINED'",
    [fixture.owner],
  );
  await database.query("UPDATE recovery_state SET quarantined=false");
  multica = start("/multica", [], mcEnv);
  multica.stdout.resume();
  await waitHTTP("http://127.0.0.1:8030/health");
  const capability = await request(nativeOrigin + "/v1/compatibility-capabilities", "GET", {
    Authorization: "Bearer native-admin-fixture",
  });
  assert.equal(capability.status, 200);
  assert.equal(JSON.parse(capability.body).data.securityRevision, 2);
  const gatewayArgs = [
    "-database-url-file",
    "/tmp/database",
    "-control-listen",
    "127.0.0.1:8080",
    "-mcp-listen",
    "127.0.0.1:8081",
    "-public-origin",
    "http://127.0.0.1:8080",
    "-mcp-origin",
    "http://127.0.0.1:8081",
    "-native-url",
    nativeOrigin,
    "-native-admin-file",
    "/tmp/native",
    "-vault-key-file",
    "/tmp/vault",
    "-source-origin",
    "http://127.0.0.1:8030",
    "-source-token-file",
    "/tmp/source",
  ];
  gateway = start("/gateway", gatewayArgs);
  gateway.stdout.resume();
  await waitHTTP("http://127.0.0.1:8080/health");
  const oldEndpoint = "http://127.0.0.1:8081" + new URL(fixture.endpoint).pathname;
  assert.equal((await mcp(oldEndpoint, fixture.bearer, "tools/list", {})).status, 401);
  const issued = await request(
    "http://127.0.0.1:8080/api/v3.1/tool_router/session",
    "POST",
    { "x-api-key": "control-fixture", "Content-Type": "application/json" },
    {
      user_id: fixture.owner,
      toolkits: { enable: ["example"] },
      connected_accounts: { example: ["ca_fixture"] },
      compat_context: { agent_id: fixture.agent, actor_user_id: fixture.owner, task_id: fixture.task },
    },
  );
  assert.equal(issued.status, 200, "fresh scoped session issuance");
  const session = JSON.parse(issued.body).mcp;
  const bearer = session.headers.Authorization.slice(7);
  assert.equal((await request("http://127.0.0.1:8080/api/v3.1/toolkits", "GET", { "x-api-key": bearer })).status, 401);
  assert.equal(
    (await request(nativeOrigin + "/v1/connections", "GET", { Authorization: "Bearer " + bearer })).status,
    401,
  );
  console.log("PASS actual session credential cannot enter gateway control or native administration");
  const execute = (operationId, value) =>
    mcp(session.url, bearer, "tools/call", {
      name: "execute_action",
      arguments: { actionId: "example.read", connectionName: "ca_fixture", operationId, input: { value } },
    });
  for (let i = 0; i < 2; i++) {
    const result = await execute("isolated-canary-read", "canary-read");
    assert.equal(result.status, 200);
    assert.ok(result.body.includes("canary-read"));
    if (i === 0)
      await database.query("UPDATE execution SET created_at=now()-interval '90 days' WHERE state='SUCCEEDED'");
  }
  assert.equal((await recovery()).executions, 1);
  for (let i = 0; i < 2; i++) {
    await execute("isolated-canary-lost", "__lose_response__");
    if (i === 0) await database.query("UPDATE execution SET created_at=now()-interval '90 days' WHERE state='UNKNOWN'");
  }
  assert.equal((await recovery()).executions, 2);
  assert.equal((await database.query("SELECT count(*)::int AS n FROM execution WHERE state='UNKNOWN'")).rows[0].n, 1);
  console.log(
    "PASS actual Multica server authority -> gateway -> encrypted native OC: fresh scoped read, replay, UNKNOWN no redispatch",
  );

  await stop(multica);
  assert.equal((await mcp(session.url, bearer, "tools/list", {})).status, 401);
  console.log("PASS unavailable live authority denies existing session; 90-day ledger records do not redispatch");
  const rollback = { ...mcEnv, COMPOSIO_BACKEND: "official", COMPOSIO_API_KEY: "", FF_COMPOSIO_MCP_APPS: "false" };
  multica = start("/multica", [], rollback);
  multica.stdout.resume();
  await waitHTTP("http://127.0.0.1:8030/health");
  assert.equal((await mcp(session.url, bearer, "tools/list", {})).status, 401);
  assert.deepEqual(
    (await recovery()).connections.find((row) => row.id === orphanID),
    orphan,
  );
  assert.equal((await recovery()).executions, 2);
  assert.equal((await database.query("SELECT count(*)::int AS n FROM execution WHERE state='UNKNOWN'")).rows[0].n, 1);
  await stop(gateway);
  gateway = start("/gateway", gatewayArgs);
  gateway.stdout.resume();
  await waitHTTP("http://127.0.0.1:8080/health");
  assert.equal((await mcp(session.url, bearer, "tools/list", {})).status, 401);
  console.log(
    "PASS rollback: compatibility disabled, current and restarted gateway deny bearer; credentials and UNKNOWN retained; no official credentials reused",
  );
} finally {
  for (const child of children) await stop(child);
  await database.end();
  bridge.close();
}
