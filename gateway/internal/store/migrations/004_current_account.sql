CREATE UNIQUE INDEX CONCURRENTLY connection_current_account ON connection(project_id,subject_id,toolkit) WHERE state = 'ACTIVE';
