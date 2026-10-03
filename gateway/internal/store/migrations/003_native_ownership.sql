CREATE UNIQUE INDEX CONCURRENTLY connection_native_ownership ON connection(runtime_id,native_id) WHERE state <> 'DELETED';
