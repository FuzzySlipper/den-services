# Artifacts Service

`artifacts` is the planned centralized Den artifact registry and blob service. It owns metadata for durable screenshot/image evidence and keeps raw bytes outside Den task, message, review, and document tables.

The service boundary follows the `den-services/den-artifact-centralized-image-storage-plan` Den document:

- metadata lives in Postgres schema `den_artifacts`;
- blob bytes live in a configured backend, initially filesystem/NFS;
- agents and review packets use stable `den-artifact://...` refs;
- raw image bytes and base64 payloads do not belong in Den workflow tables.

## Current Scaffold

This task adds the deployable service shell, DDL, configuration, and API contract routes. The upload/read/delete routes intentionally return `501 not_implemented` until the blob backend and store implementation land.

Implemented now:

- `GET /health`
- `GET /version`
- authenticated API mux
- route prototypes for metadata create/read, content read, optional thumbnail read, and delete/tombstone
- filesystem backend configuration surface
- `den_artifacts` schema DDL

## Planned API

```text
POST /v1/artifacts
  Upload raw bytes or multipart content plus metadata.

GET /v1/artifacts/{artifact_id}/metadata
  Return metadata only.

GET /v1/artifacts/{artifact_id}/content
  Return bytes with content-type after auth checks.

GET /v1/artifacts/{artifact_id}/thumbnail
  Optional derived preview for Den Web.

DELETE /v1/artifacts/{artifact_id}
  Tombstone metadata and schedule blob deletion according to retention policy.
```

The canonical ref form is:

```text
den-artifact://<artifact_id>
```

Human-readable scoped refs may also be supported:

```text
den-artifact://den-services/tasks/3424/screenshots/agora-overview.png
```

The resolver must normalize either form to one artifact metadata row.

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

The backend interface should stay small enough to add MinIO/S3 later without changing API clients.

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
