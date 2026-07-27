# web-edge

`web-edge` is Den's stateless browser edge. It serves the active `den-web`
asset release and forwards same-origin `/api/v1/*` requests to Gateway.

It intentionally does not know service-owner URLs or domain credentials.
Gateway owns route selection and replaces the edge's dedicated caller token
with the destination service token.

Production inputs:

- config: `/data/services/web-edge/config/config.yaml`;
- secrets: `/etc/den-services/web-edge.env`;
- assets: `/data/services/den-web/wwwroot`;
- unit: `den-go@web-edge.service`.

Health and version describe the Go edge build:

```bash
curl -fsS http://127.0.0.1:18080/health
curl -fsS http://127.0.0.1:18080/version
```

The separately deployed frontend build is reported by:

```bash
curl -fsS http://127.0.0.1:18080/den-web-build.json
```

Run focused verification with:

```bash
go test -race -count=1 ./web-edge/... ./gateway/...
```
