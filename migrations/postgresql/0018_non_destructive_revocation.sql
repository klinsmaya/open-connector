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
create function guard_provider_revocation(target_service text, deleting_connection boolean default false) returns void as $$
declare current_state text;
begin
 if target_service is null then
  if exists(select 1 from provider_revocation_barriers where state is not null) then raise exception 'provider_revocation_barrier'; end if;
  return;
 end if;
 insert into provider_revocation_barriers(service) values(target_service) on conflict do nothing;
 select state into current_state from provider_revocation_barriers where service=target_service for update;
 if current_state is not null then raise exception 'provider_revocation_barrier'; end if;
end;
$$ language plpgsql;
create function guard_provider_revocation_row() returns trigger as $$
begin
 if TG_OP='DELETE' then
  perform guard_provider_revocation(OLD.service,TG_TABLE_NAME='connections'); return OLD;
 end if;
 if TG_TABLE_NAME='connections' and TG_OP='UPDATE' and NEW is not distinct from OLD then return NEW; end if;
 if TG_OP='UPDATE' then perform guard_provider_revocation(OLD.service,false); end if;
 perform guard_provider_revocation(NEW.service,false);
 return NEW;
end;
$$ language plpgsql;
create trigger revocation_guard_connections before insert or update or delete on connections for each row execute function guard_provider_revocation_row();
create trigger revocation_guard_oauth_client_configs before insert or update or delete on oauth_client_configs for each row execute function guard_provider_revocation_row();
create trigger revocation_guard_oauth_states before insert or update on oauth_states for each row execute function guard_provider_revocation_row();
create trigger revocation_guard_connection_requests before insert or update on connection_requests for each row execute function guard_provider_revocation_row();
create function guard_provider_revocation_trigger() returns trigger as $$
declare provider_service text;
begin
 if TG_OP='UPDATE' and NEW.connection_id=OLD.connection_id and NEW.status not in ('active','deleting') then return NEW; end if;
 select service into provider_service from connections where id=NEW.connection_id;
 if provider_service is not null then perform guard_provider_revocation(provider_service,false); end if;
 return NEW;
end;
$$ language plpgsql;
create trigger revocation_guard_subscription before insert or update of connection_id, status on trigger_subscriptions for each row execute function guard_provider_revocation_trigger();
