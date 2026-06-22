# doc-publish

`doc-publish` publishes canonical Den documents into one configured
Jekyll/GitHub Pages blog repository. It is intentionally separate from the old
`den-publish` repo/tooling: this is a den-services Go authority that owns the
filesystem/git mutation workflow and publication audit records.

## API

All `/v1/blog/*` routes require the `DEN_DOC_PUBLISH_SERVICE_TOKEN` bearer token
when called directly. Gateway should expose caller tokens and forward the
service token upstream.

Preview a publication without writing the blog repo:

```bash
curl -fsS \
  -H "Authorization: Bearer ${DEN_DOC_PUBLISH_SERVICE_TOKEN}" \
  -H "Content-Type: application/json" \
  --data @/tmp/doc-publish-preview.json \
  http://127.0.0.1:8087/v1/blog/publications/preview
```

Publish after confirmation:

```bash
curl -fsS \
  -H "Authorization: Bearer ${DEN_DOC_PUBLISH_SERVICE_TOKEN}" \
  -H "Content-Type: application/json" \
  --data @/tmp/doc-publish-request.json \
  http://127.0.0.1:8087/v1/blog/publications
```

Read an audit record:

```bash
curl -fsS \
  -H "Authorization: Bearer ${DEN_DOC_PUBLISH_SERVICE_TOKEN}" \
  http://127.0.0.1:8087/v1/blog/publications/{publication_id}
```

## Request Shape

```json
{
  "source": {
    "project_id": "den-web",
    "document_project_id": "den-web",
    "document_slug": "example-doc"
  },
  "options": {
    "title": "optional override",
    "slug": "optional-override",
    "tags": ["den"],
    "overwrite": false
  },
  "requested_by": "pi",
  "document": {
    "title": "Preview-only title",
    "markdown": "Preview-only markdown"
  }
}
```

`document` payloads are for preview ergonomics only. Publish fetches the
canonical source document server-side from the configured Core/Gateway document
surface before writing the blog repo.

On den-srv, set `source.documents_base_url` in
`/data/services/doc-publish/config/config.yaml` to `http://127.0.0.1:5299` so
publish can read canonical documents from Den Core. Preview may still work with
an inline `document` payload when this URL is wrong, but publish will fail while
fetching the source document.

## Safety Model

Publishing is fail-closed:

- one configured repo path;
- expected origin remote and branch are validated before writing;
- dirty repos are rejected;
- generated paths must stay under the configured post directory;
- duplicate post paths fail unless `overwrite` is true or an existing
  publication record for the same source document is being updated;
- git commands use `exec.CommandContext` with argv arrays and bounded timeouts;
- dry-run/preview does not write, commit, or push.

Generated posts use Jekyll/GitHub Pages format:

```text
_posts/YYYY-MM-DD-slug.md
```

Frontmatter includes `layout: post`, escaped `title`, `date`, and optional
slugified tags. Existing source frontmatter is stripped before rendering.
