create schema if not exists den_artifacts;

comment on schema den_artifacts is
  'Artifact metadata registry. Blob bytes live outside Den workflow tables in configured blob storage.';

create table den_artifacts.artifacts (
    artifact_id text primary key,
    project_id text,
    task_id bigint,
    review_round_id bigint,
    finding_id bigint,
    owner_kind text,
    owner_id text,
    logical_name text not null,
    mime_type text not null,
    byte_count bigint not null check (byte_count >= 0),
    sha256 text not null,
    width integer check (width is null or width > 0),
    height integer check (height is null or height > 0),
    sensitive boolean not null default false,
    storage_backend text not null,
    storage_key text not null,
    created_by text,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    expires_at timestamptz,
    deleted_at timestamptz,
    deletion_reason text,
    unique(storage_backend, storage_key),
    check (length(sha256) = 64)
);

create index idx_artifacts_project_task on den_artifacts.artifacts(project_id, task_id) where deleted_at is null;
create index idx_artifacts_review on den_artifacts.artifacts(review_round_id, finding_id) where deleted_at is null;
create index idx_artifacts_owner on den_artifacts.artifacts(owner_kind, owner_id) where deleted_at is null;
create index idx_artifacts_sha256 on den_artifacts.artifacts(sha256) where deleted_at is null;
create index idx_artifacts_expiration on den_artifacts.artifacts(expires_at) where expires_at is not null and deleted_at is null;
create index idx_artifacts_deleted on den_artifacts.artifacts(deleted_at) where deleted_at is not null;

comment on table den_artifacts.artifacts is
  'Metadata rows for Den artifacts. Raw blob bytes/base64 are not stored in task, message, review, document, or artifact metadata tables.';

comment on column den_artifacts.artifacts.storage_backend is
  'Blob backend identifier, initially filesystem.';

comment on column den_artifacts.artifacts.storage_key is
  'Backend-local blob key, e.g. sha256/ab/cd/<hash>.';

comment on column den_artifacts.artifacts.sensitive is
  'Caller-provided handling flag. Sensitive artifacts should avoid broad preview exposure.';

grant usage on schema den_artifacts to den_artifacts_app;
grant select, insert, update, delete on den_artifacts.artifacts to den_artifacts_app;
