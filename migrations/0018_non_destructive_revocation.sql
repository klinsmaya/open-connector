-- Non-destructive revocation uses persistent barriers, never expiring leases.
create table provider_revocation_barriers (
 service text primary key,
 state text check(state in ('REVOKING','REVOKED','UNKNOWN','UNSUPPORTED')),
 connection_id text,
 connection_revision text,
 operation_id text
);
create table provider_oauth_operations (
 id text primary key,
 service text not null,
 created_at text not null,
 value text
);
create index provider_oauth_operations_service on provider_oauth_operations(service);
alter table oauth_states add column service text;
create trigger revocation_guard_connections_insert before insert on connections begin
 select raise(abort,'provider_revocation_barrier') where exists(select 1 from provider_revocation_barriers where service=new.service and state is not null);
end;
create trigger revocation_guard_connections_update before update on connections when new.revision is not old.revision or new.value is not old.value or new.service is not old.service or new.connection_name is not old.connection_name begin
 select raise(abort,'provider_revocation_barrier') where exists(select 1 from provider_revocation_barriers where (service=new.service or service=old.service) and state is not null);
end;
create trigger revocation_guard_oauth_client_configs_insert before insert on oauth_client_configs begin
 select raise(abort,'provider_revocation_barrier') where exists(select 1 from provider_revocation_barriers where service=new.service and state is not null);
end;
create trigger revocation_guard_oauth_client_configs_update before update on oauth_client_configs begin
 select raise(abort,'provider_revocation_barrier') where exists(select 1 from provider_revocation_barriers where (service=new.service or service=old.service) and state is not null);
end;
create trigger revocation_guard_oauth_states_insert before insert on oauth_states begin
 select raise(abort,'provider_revocation_barrier') where exists(select 1 from provider_revocation_barriers where (service=new.service or new.service is null) and state is not null);
end;
create trigger revocation_guard_oauth_states_update before update on oauth_states begin
 select raise(abort,'provider_revocation_barrier') where exists(select 1 from provider_revocation_barriers where (service=new.service or service=old.service or new.service is null) and state is not null);
end;
create trigger revocation_guard_connection_requests_insert before insert on connection_requests begin
 select raise(abort,'provider_revocation_barrier') where exists(select 1 from provider_revocation_barriers where service=new.service and state is not null);
end;
create trigger revocation_guard_connection_requests_update before update on connection_requests begin
 select raise(abort,'provider_revocation_barrier') where exists(select 1 from provider_revocation_barriers where (service=new.service or service=old.service) and state is not null);
end;
create trigger revocation_guard_connections_delete before delete on connections begin
 select raise(abort,'provider_revocation_barrier') where exists(select 1 from provider_revocation_barriers where service=old.service and state is not null);
end;
create trigger revocation_guard_config_delete before delete on oauth_client_configs begin
 select raise(abort,'provider_revocation_barrier') where exists(select 1 from provider_revocation_barriers where service=old.service and state is not null);
end;
create trigger revocation_guard_trigger_insert before insert on trigger_subscriptions begin
 select raise(abort,'provider_revocation_barrier') where exists(select 1 from connections c join provider_revocation_barriers b on b.service=c.service where c.id=new.connection_id and b.state is not null);
end;
create trigger revocation_guard_trigger_update before update of connection_id, status on trigger_subscriptions when new.connection_id is not old.connection_id or new.status in ('active','deleting') begin
 select raise(abort,'provider_revocation_barrier') where exists(select 1 from connections c join provider_revocation_barriers b on b.service=c.service where c.id=new.connection_id and b.state is not null);
end;
