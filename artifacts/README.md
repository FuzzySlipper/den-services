# Artifacts Service

`artifacts` is the planned centralized Den artifact registry and blob service. It owns metadata for durable screenshot/image evidence and keeps raw bytes outside Den task, message, review, and document tables.

The service boundary follows the `den-services/den-artifact-centralized-image-storage-plan` Den document:

- metadata lives in Postgres schema `den_artifacts`;
- blob bytes live in a configured backend, initially filesystem/NFS;
- agents and review packets use stable `den-artifact://...` refs;
- raw image bytes and base64 payloads do not belong in Den workflow tables.

## Current Implementation

The service has a deployable HTTP shell, Postgres metadata store, and initial filesystem blob backend.

Implemented now:

- `GET /health`
- `GET /version`
- authenticated API mux
- multipart upload
- metadata read
- content read
- delete/tombstone
- `artifact-upload` CLI helper
- filesystem backend configuration surface
- `den_artifacts` schema DDL

## API

```text
POST /v1/artifacts
  Multipart upload. File part name: file.
  Metadata form fields:
    project_id, task_id, review_round_id, finding_id
    owner_kind, owner_id
    logical_name
    mime_type
    sensitive=true|false
    created_by
    temporary=true|false

GET /v1/artifacts/{artifact_id}/metadata
  Return metadata only.

GET /v1/artifacts/resolve?ref=den-artifact://...
  Resolve canonical or scoped refs to metadata.

GET /v1/artifacts/{artifact_id}/content
  Return bytes with content-type after auth checks.

DELETE /v1/artifacts/{artifact_id}
  Tombstone metadata. Content and metadata reads hide tombstoned artifacts.
```

The canonical ref form is:

```text
den-artifact://<artifact_id>
```

Responses include a human-readable scoped ref when project/task metadata is present:

```text
den-artifact://den-services/tasks/3424/artifacts/agora-overview.png
```

The resolver normalizes either form to one artifact metadata row. Scoped refs resolve by project ID, task ID, and logical name.

## Storage Model

Metadata rows store:

- artifact ID;
- project/task/review/finding refs;
- owner kind and owner ID;
- logical name;
- MIME type;
- byte count and SHA-256;
- image dimensions;
- sensitivity flag;
- storage backend and key;
- creator;
- created/updated timestamps;
- expiration and deletion state.

Initial blob backend:

```text
/var/lib/den/artifacts/sha256/ab/cd/<sha256>
```

The filesystem backend stores blobs by SHA-256. Duplicate uploads with different owners create separate metadata rows pointing at the same content-addressed blob key.

## Upload Example

Agent-friendly helper:

```bash
export DEN_ARTIFACTS_BASE_URL=http://127.0.0.1:8090
export DEN_ARTIFACTS_SERVICE_TOKEN=<service-token>

go run ./artifacts/cmd/artifact-upload \
  -file /tmp/den-visual-inspect/agora-overview.png \
  -project-id den-services \
  -task-id 3478 \
  -logical-name agora-overview.png \
  -created-by agent-name
```

The helper prints the artifact metadata JSON returned by the service, including `artifact_id`, `artifact_ref`, MIME type, dimensions, SHA-256, and byte count. It does not print raw image bytes.

Raw multipart equivalent:

```bash
curl -sS \
  -H "Authorization: Bearer $DEN_ARTIFACTS_SERVICE_TOKEN" \
  -F "file=@screenshot.png;type=image/png" \
  -F "project_id=den-services" \
  -F "task_id=3476" \
  -F "logical_name=screenshot.png" \
  -F "created_by=agent-name" \
  http://127.0.0.1:8090/v1/artifacts
```

## Visual Inspect Flow

1. Capture a screenshot to a local PNG/JPEG path.
2. Upload it with `go run ./artifacts/cmd/artifact-upload ...`.
3. Copy `artifact_ref` from the JSON output into `visual-inspect` as `screenshots[].ref`.
4. Post only the visual-inspect result JSON plus artifact refs to Den review messages.

Example request fragment:

```json
{
  "screenshots": [
    {
      "id": "overview",
      "ref": "den-artifact://art_01jexample",
      "mime_type": "image/png",
      "description": "Overview after selecting the terminal card"
    }
  ]
}
```

Content read:

```bash
curl -sS \
  -H "Authorization: Bearer $DEN_ARTIFACTS_SERVICE_TOKEN" \
  http://127.0.0.1:8090/v1/artifacts/<artifact_id>/content \
  -o artifact.png
```

## Visual Inspect Contract

`visual-inspect` remains stateless. It should resolve `den-artifact://...` by calling this service, then enforce its own max-byte and max-pixel limits after fetch.

Review messages should include refs and result JSON only:

```json
{
  "artifact_refs": [
    {
      "screenshot_id": "overview",
      "ref": "den-artifact://art_01j...",
      "mime_type": "image/png",
      "sensitive": false
    }
  ],
  "result": {}
}
```

Do not store raw PNG/JPEG bytes, data URLs, or base64 strings in Den task/message/review tables.
