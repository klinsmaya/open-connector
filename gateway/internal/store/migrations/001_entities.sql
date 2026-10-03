CREATE TABLE project (
 id text PRIMARY KEY,
 control_digest bytea NOT NULL CHECK (octet_length(control_digest) = 32),
 enabled boolean NOT NULL DEFAULT false,
 callback_origins text[] NOT NULL DEFAULT '{}'
);
CREATE TABLE subject (
 project_id text NOT NULL,
 id text NOT NULL,
 enabled boolean NOT NULL DEFAULT true,
 PRIMARY KEY (project_id, id)
);
CREATE TABLE auth_config (
 project_id text NOT NULL,
 id text NOT NULL,
 toolkit text NOT NULL,
 display_name text NOT NULL,
 runtime_id text NOT NULL,
 auth_type text NOT NULL,
 revision bigint NOT NULL DEFAULT 1,
 enabled boolean NOT NULL DEFAULT false,
 approved_actions text[] NOT NULL DEFAULT '{}',
 capabilities jsonb NOT NULL DEFAULT '{}',
 PRIMARY KEY (project_id, id)
);
CREATE TABLE connection (
 project_id text NOT NULL,
 id text NOT NULL,
 subject_id text NOT NULL,
 auth_config_id text NOT NULL,
 toolkit text NOT NULL,
 runtime_id text NOT NULL,
 native_id text NOT NULL,
 state text NOT NULL CHECK (state IN ('PENDING','ACTIVE','REVOKING','REVOKED','QUARANTINED','DELETED')),
 generation bigint NOT NULL DEFAULT 1,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (project_id,id)
);
CREATE TABLE agent_binding (
 project_id text NOT NULL,
 agent_id text NOT NULL,
 subject_id text NOT NULL,
 generation bigint NOT NULL DEFAULT 1,
 enabled boolean NOT NULL DEFAULT false,
 connection_ids text[] NOT NULL DEFAULT '{}',
 action_ids text[] NOT NULL DEFAULT '{}',
 PRIMARY KEY (project_id,agent_id)
);
CREATE TABLE session (
 project_id text NOT NULL,
 id text NOT NULL,
 subject_id text NOT NULL,
 agent_id text NOT NULL,
 actor_id text NOT NULL,
 task_id text NOT NULL,
 grant_generation bigint NOT NULL,
 bearer_digest bytea NOT NULL CHECK (octet_length(bearer_digest)=32),
 state text NOT NULL CHECK (state IN ('PENDING','ACTIVE','REVOKED','QUARANTINED')),
 expires_at timestamptz NOT NULL,
 issued_at timestamptz NOT NULL DEFAULT now(),
 runtime_token_id text,
 runtime_ciphertext bytea,
 PRIMARY KEY (project_id,id),
 CHECK (state <> 'ACTIVE' OR (runtime_token_id IS NOT NULL AND runtime_ciphertext IS NOT NULL))
);
CREATE TABLE session_grant (
 project_id text NOT NULL,
 session_id text NOT NULL,
 connection_id text NOT NULL,
 connection_generation bigint NOT NULL,
 action_id text NOT NULL,
 PRIMARY KEY (project_id,session_id,connection_id,action_id)
);
CREATE TABLE connect_transaction (
 project_id text NOT NULL,
 id text NOT NULL,
 subject_id text NOT NULL,
 auth_config_id text NOT NULL,
 callback_url text NOT NULL,
 nonce_digest bytea NOT NULL,
 native_request_id text,
 native_connection_id text,
 phase text NOT NULL CHECK (phase IN ('PENDING','CREATING','VERIFYING','ACTIVE','FAILED','EXPIRED','SUPERSEDED','QUARANTINED')),
 version bigint NOT NULL DEFAULT 1,
 expires_at timestamptz NOT NULL,
 PRIMARY KEY (project_id,id)
);
CREATE TABLE execution (
 project_id text NOT NULL,
 id text NOT NULL,
 session_id text NOT NULL,
 connection_id text NOT NULL,
 action_id text NOT NULL,
 fingerprint bytea NOT NULL,
 state text NOT NULL CHECK (state IN ('PREPARED','DISPATCHED','SUCCEEDED','FAILED','UNKNOWN','QUARANTINED')),
 created_at timestamptz NOT NULL DEFAULT now(),
 native_execution_id text,
 result jsonb,
 PRIMARY KEY (project_id,id)
);
CREATE TABLE outbox_job (
 project_id text NOT NULL,
 id text NOT NULL,
 kind text NOT NULL,
 resource_id text NOT NULL,
 state text NOT NULL DEFAULT 'PENDING' CHECK (state IN ('PENDING','LEASED','DONE','BLOCKED')),
 attempts integer NOT NULL DEFAULT 0,
 available_at timestamptz NOT NULL DEFAULT now(),
 lease_until timestamptz,
 last_error_code text,
 PRIMARY KEY (project_id,id)
);
CREATE TABLE audit_event (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 project_id text NOT NULL,
 subject_id text,
 actor_id text,
 resource_id text NOT NULL,
 event text NOT NULL,
 result text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE recovery_state (
 singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
 quarantined boolean NOT NULL DEFAULT true
);
INSERT INTO recovery_state(singleton,quarantined) VALUES (true,true);
