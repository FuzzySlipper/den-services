alter table den_projects.projects
    add column repository_url text;

comment on column den_projects.projects.repository_url is
  'Optional HTTPS, SSH, or scp-style Git remote URL for locating matching local checkouts.';

create or replace view den_projects.project_refs as
select id, kind, visibility, owner, root_path, updated_at, repository_url
from den_projects.projects;

create or replace view den_projects.visible_projects as
select id, name, kind, visibility, owner, root_path, description, settings_json, created_at, updated_at, repository_url
from den_projects.projects
where kind = 'project'
  and visibility = 'normal';

create or replace view den_projects.visible_spaces as
select id, name, kind, visibility, owner, root_path, description, settings_json, created_at, updated_at, repository_url
from den_projects.projects
where visibility not in ('hidden', 'archived');
