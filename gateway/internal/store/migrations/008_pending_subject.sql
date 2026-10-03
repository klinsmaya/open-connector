CREATE UNIQUE INDEX CONCURRENTLY connect_pending_subject ON connect_transaction(project_id,subject_id,auth_config_id) WHERE phase IN ('PENDING','CREATING','VERIFYING');
