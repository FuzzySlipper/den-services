do $$
begin
    if exists (select 1 from pg_roles where rolname = 'den_projects_app') then
        grant delete on den_projects.projects to den_projects_app;
    end if;
end $$;
