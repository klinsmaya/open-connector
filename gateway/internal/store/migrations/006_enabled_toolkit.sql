CREATE UNIQUE INDEX CONCURRENTLY auth_config_enabled_toolkit ON auth_config(project_id,toolkit) WHERE enabled;
