CREATE TABLE task_revocation (
 project_id text NOT NULL,
 task_id text NOT NULL,
 revoked boolean NOT NULL DEFAULT false,
 PRIMARY KEY (project_id,task_id)
);
