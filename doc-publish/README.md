# doc-publish

`doc-publish` publishes canonical Den documents into one configured
Jekyll/GitHub Pages blog repository. It is intentionally separate from the old
`den-publish` repo/tooling: this is a den-services Go authority that owns the
filesystem/git mutation workflow and publication audit records.

## API

The `/v1/blog/*` routes are served without inbound authentication; doc-publish
runs on the trusted Den LAN.

Preview a publication without writing the blog repo:

```bash
curl -fsS \
  -H "Content-Type: application/json" \
  --data @/tmp/doc-publish-preview.json \
  http://127.0.0.1:8087/v1/blog/publications/preview
```

Publish after confirmation:

```bash
curl -fsS \
  -H "Content-Type: application/json" \
  --data @/tmp/doc-publish-request.json \
  http://127.0.0.1:8087/v1/blog/publications
```

Read an audit record:

```bash
curl -fsS \
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
`/data/services/doc-publish/config/config.yaml` to the canonical documents
service, normally `http://127.0.0.1:8094`. The fetcher also preserves the
older Core route for legacy configs. Preview may still work with an inline
`document` payload when this URL is wrong, but publish will
fail while fetching the source document.

## Wiki Pages

A request can name one den-wiki page instead of a Den document:

```json
{
  "source": { "wiki_page": "wiki/agent-memory.md" },
  "options": { "tags": ["research"] },
  "requested_by": "claude-code"
}
```

den-wiki's `wiki_publish` operation sends this; people ask an agent to share
a page rather than use OpenKnowledge's Share button. Only that page is
published, never the pages it links to. Preview and publish both fetch the page
server-side from `{wiki.base_url}/v1/export?page=...&asset_base=...`; den-wiki
renders it for outside readers (citations linked to the original sources, other
wiki pages as plain text, images listed as assets) and returns warnings for
what it left out, which the response carries. A request naming both a wiki
page and a document, or a wiki page with an inline `document`, is rejected.

The page's images go to `{blog.asset_dir}/<key>/<name>`, where `<key>` is the
page path without `wiki/` and `.md`, slugified (`wiki/ai/agent-memory.md` →
`ai-agent-memory`); the Markdown links them at
`{blog.public_base_url}/{blog.asset_dir}/<key>/<name>`. Preview lists those
paths as `asset_paths` without fetching the images. Publish fetches each from
`/v1/export/asset`, rejects any over `wiki.max_asset_bytes` or not matching its
sha256, and commits them with the post in one commit. Publishing the same page
again updates its post and images in place: the post keeps its first slug,
date, path and URL even if the page's title changed, and only the title and
body change; an image the page no longer uses stays in
the blog repo.

Wiki pages are disabled until `wiki.base_url` is set, and need
`blog.asset_dir`, `wiki.request_timeout` and `wiki.max_asset_bytes`; see
`config/config.example.yaml`.

## Safety Model

Publishing is fail-closed:

- one configured repo path;
- expected origin remote and branch are validated before writing;
- dirty repos are rejected;
- generated paths must stay under the configured post directory, and wiki
  images under the configured asset directory;
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
