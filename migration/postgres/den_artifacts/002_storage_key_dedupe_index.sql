alter table den_artifacts.artifacts
drop constraint if exists artifacts_storage_backend_storage_key_key;

create index if not exists idx_artifacts_storage_key
on den_artifacts.artifacts(storage_backend, storage_key)
where deleted_at is null;
