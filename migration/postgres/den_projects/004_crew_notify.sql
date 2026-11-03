alter table den_projects.projects
    add column crew_notify_json jsonb;

comment on column den_projects.projects.crew_notify_json is
  'Optional crew addresses to tell about the project''s tasks, by event ({"on_done": [{"address", "policy"}]}). Read by crew-review; Den checks only the shape.';

create or replace view den_projects.visible_projects as
select id, name, kind, visibility, owner, root_path, description, settings_json, created_at, updated_at, repository_url, crew_notify_json
from den_projects.projects
where kind = 'project'
  and visibility = 'normal';

create or replace view den_projects.visible_spaces as
select id, name, kind, visibility, owner, root_path, description, settings_json, created_at, updated_at, repository_url, crew_notify_json
from den_projects.projects
where visibility not in ('hidden', 'archived');
