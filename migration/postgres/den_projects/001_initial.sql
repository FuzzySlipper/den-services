create schema if not exists den_projects;

comment on schema den_projects is
  'Project, space, and scope metadata registry for Den lifeboat services.';

create table den_projects.projects (
    id text primary key,
    name text not null,
    kind text not null default 'project',
    visibility text not null default 'normal',
    owner text,
    root_path text,
    description text,
    settings_json jsonb,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

create index idx_den_projects_kind_visibility on den_projects.projects(kind, visibility);
create index idx_den_projects_visibility on den_projects.projects(visibility);
create index idx_den_projects_root_path on den_projects.projects(root_path) where root_path is not null;

comment on table den_projects.projects is
  'Canonical scope registry. Project rows use kind=project; non-project kinds are spaces.';

comment on column den_projects.projects.id is
  'Stable cross-domain scope identifier. Dependent services store this as project_id or space_id.';

comment on column den_projects.projects.kind is
  'Application-validated scope kind, initially project, personal, assistant, knowledge_base, or system.';

comment on column den_projects.projects.visibility is
  'Application-validated visibility, initially normal, hidden, or archived.';

create view den_projects.project_refs as
select id, kind, visibility, owner, root_path, updated_at
from den_projects.projects;

create view den_projects.visible_projects as
select id, name, kind, visibility, owner, root_path, description, settings_json, created_at, updated_at
from den_projects.projects
where kind = 'project'
  and visibility = 'normal';

create view den_projects.visible_spaces as
select id, name, kind, visibility, owner, root_path, description, settings_json, created_at, updated_at
from den_projects.projects
where visibility not in ('hidden', 'archived');

do $$
begin
    if exists (select 1 from pg_roles where rolname = 'den_projects_app') then
        grant usage on schema den_projects to den_projects_app;
        grant select on den_projects.project_refs to den_projects_app;
        grant select on den_projects.visible_projects to den_projects_app;
        grant select on den_projects.visible_spaces to den_projects_app;
        grant select, insert, update on den_projects.projects to den_projects_app;
        alter default privileges in schema den_projects grant select, insert, update on tables to den_projects_app;
    end if;
end $$;
