# visual-inspect

`visual-inspect` is a stateless vision-LLM judgment service for Den review gates. It answers focused visual questions against screenshot artifact refs, such as whether a terminal card is visibly focused or whether an overview shows exactly three cards.

Use this service for semantic visual evidence. Use `visual-contract` when the check is a deterministic structural contract over collected browser/layout data. `visual-inspect` can say "this screenshot appears to show the terminal focused"; `visual-contract` should say "this selector exists, this text is visible, these boxes have this relationship."

## Current API

`POST /v1/visual-inspect/evaluate` runs:

1. request validation;
2. artifact/image fetch from configured refs;
3. stateless evaluator call;
4. response parsing and fail-closed normalization.

Valid evaluations return `200` even when the verdict is `uncertain`.

`POST /v1/visual-inspect/describe` uses the same artifact fetch and model path, but returns a plain-language description instead of a scored review packet. Use it when an agent or downstream model needs a visual summary but does not need pass/fail criteria.

```bash
cd visual-inspect
export VISUAL_INSPECT_CONFIG_PATH=config.example.yaml
export DEN_VISUAL_INSPECT_TOKEN=local-review-token
export DEN_VISUAL_INSPECT_LLM_BASE_URL=http://127.0.0.1:18141/v1
export DEN_VISUAL_INSPECT_LLM_API_KEY=local-fake-key
go run ./cmd/visual-inspect
```

```bash
curl -sS http://127.0.0.1:18140/v1/visual-inspect/evaluate \
  -H 'Authorization: Bearer local-review-token' \
  -H 'Content-Type: application/json' \
  --data @examples/agora-terminal-selected.request.json
```

```bash
curl -sS http://127.0.0.1:18140/v1/visual-inspect/describe \
  -H 'Authorization: Bearer local-review-token' \
  -H 'Content-Type: application/json' \
  --data @examples/agora-describe.request.json
```

From the repo root, use `--data @visual-inspect/examples/agora-terminal-selected.request.json`.

## Fake Local Evaluator Smoke

For local endpoint smoke without a real model, run an OpenAI-compatible fake responder on `127.0.0.1:18141`:

```bash
python3 - <<'PY'
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json

class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        self.rfile.read(int(self.headers.get("content-length", "0")))
        result = {
            "request_id": "task-3317-terminal-selected",
            "verdict": "pass",
            "confidence": 0.86,
            "criteria_results": [{
                "criterion_id": "terminal-selected",
                "verdict": "pass",
                "confidence": 0.86,
                "explanation": "The terminal card has a visible selected state.",
                "observations": [{
                    "screenshot_id": "agora-overview",
                    "label": "terminal focused card",
                    "region": {"x": 0, "y": 0, "width": 2, "height": 2, "coordinate_space": "image_pixels"},
                    "confidence": 0.82
                }]
            }],
            "follow_up_hints": [],
            "model_info": {},
            "warnings": []
        }
        body = json.dumps({"choices": [{"message": {"content": json.dumps(result)}}]}).encode()
        self.send_response(200)
        self.send_header("content-type", "application/json")
        self.send_header("content-length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)
    def log_message(self, fmt, *args):
        return

ThreadingHTTPServer(("127.0.0.1", 18141), Handler).serve_forever()
PY
```

Then create or copy a PNG to `/tmp/den-visual-inspect/agora-overview.png`, start the service with the config above, and run the curl example. A request without `Authorization: Bearer local-review-token` should return `401`.

## Real LLM Config

Point the OpenAI-compatible settings at the real provider:

```bash
cd visual-inspect
export VISUAL_INSPECT_CONFIG_PATH=config.example.yaml
export DEN_VISUAL_INSPECT_TOKEN="replace-with-local-service-token"
export DEN_VISUAL_INSPECT_LLM_BASE_URL="https://api.openai.com/v1"
export DEN_VISUAL_INSPECT_LLM_API_KEY="replace-with-provider-api-key"
go run ./cmd/visual-inspect
```

Update `llm.model` in `config.example.yaml` or your deployment config to the selected vision model. Keep `llm.temperature: 0` for review gates unless GoblinBench data says otherwise.

In a second shell:

```bash
cd visual-inspect
export DEN_VISUAL_INSPECT_TOKEN="replace-with-local-service-token"
curl -sS http://127.0.0.1:18140/v1/visual-inspect/evaluate \
  -H "Authorization: Bearer ${DEN_VISUAL_INSPECT_TOKEN}" \
  -H 'Content-Type: application/json' \
  --data @examples/agora-terminal-selected.request.json
```

## Config Knobs

`config.example.yaml` is the operator-facing config surface.

- `server.listen_addr`: HTTP bind address.
- `server.read_header_timeout`: header read deadline.
- `security.service_token_env`: env var containing the bearer token.
- `security.allow_unauthenticated_local_dev`: explicit local-only bypass; keep `false` outside throwaway smoke runs.
- `artifacts.max_images`: max screenshots per request.
- `artifacts.max_bytes_per_image`: max encoded image size.
- `artifacts.max_pixels_per_image`: max decoded image pixels.
- `artifacts.allowed_schemes`: accepted ref schemes.
- `artifacts.allowed_file_roots`: allowlist for `file://` refs.
- `artifacts.artifact_service.base_url_env`: env var containing the artifacts service base URL for `den-artifact://` refs.
- `artifacts.artifact_service.service_token_env`: env var containing the artifacts service bearer token.
- `artifacts.artifact_service.timeout`: timeout for artifacts metadata/content fetches.
- `llm.provider`: currently `openai_compatible`.
- `llm.base_url_env` and `llm.api_key_env`: env var names for provider URL and secret.
- `llm.model`: model identifier sent to the provider.
- `llm.temperature`: sampling temperature.
- `llm.timeout`: provider call timeout.
- `llm.max_output_tokens`: response budget.
- `llm.max_retries`: provider retry count after the initial attempt.
- `prompts.default_profile`: profile used when a request omits `options.profile`.
- `prompts.profiles.*`: prompt files, response schema, and normalization thresholds.

Secrets stay in env vars. Tunables stay in YAML.

## Prompt Profiles

Prompt profiles are file-backed and restart-loaded in v0. A profile has:

- `system_prompt_file`: role and global judgment rules.
- `developer_prompt_file`: task-specific output discipline.
- `response_schema_file`: JSON response contract metadata/schema.
- `min_confidence_for_pass`: pass results below this become `uncertain`.
- `min_confidence_for_fail`: fail results below this become `uncertain`.

The default profile is `visual-inspect-v0`, backed by:

- `prompts/visual-inspect.system.md`
- `prompts/visual-inspect.developer.md`
- `schemas/evaluate-response.schema.json`

Model output is normalized fail-closed. Invalid JSON, unknown verdicts, invalid confidence, missing criterion results, low-confidence pass/fail results, or hidden-state inference warnings become `uncertain` rather than a silent pass.

## Model Tuning

Tune model, prompt, thresholds, timeout, and output budget together. Track at least:

- false-pass rate;
- pass/fail/uncertain accuracy;
- confidence calibration;
- coordinate usefulness;
- latency;
- cost per review gate.

Do not make `visual-inspect` a hard review gate until GoblinBench model-selection task #3463 recommends defaults for the current prompt/profile set.

## Artifact Retention And Security

`visual-inspect` does not persist screenshots. It fetches referenced images for the current request, sends them to the configured evaluator, logs bounded metadata, and returns a structured result packet. Logs include request ID, ref scheme, image size, dimensions, SHA-256, sensitivity flag, verdict, model, prompt profile, and schema version. Logs must not include raw image bytes, base64 data URLs, provider API keys, or bearer tokens.

Callers own screenshot capture and durable artifact retention. Den review messages should store artifact refs plus the result packet, not duplicate raw PNG/JPEG bytes. This keeps conversation/review history small, reduces accidental sensitive-data spread, and leaves screenshot retention policy with the artifact store.

`screenshots[].sensitive` is caller-supplied metadata. It is preserved in logs as a handling signal, but it is not automatic redaction.

## Den Review Integration

Agent-facing packet examples live in `docs/agent-usage.md`. The short version:

1. capture screenshot to an artifact location;
2. call `POST /v1/visual-inspect/evaluate` with explicit criteria and screenshot refs;
3. attach the result JSON to the Den review message or review packet;
4. include screenshot artifact refs in metadata;
5. never embed raw screenshot bytes, data URLs, or base64 image payloads in the Den message.

For local development, `file://` refs under the configured `allowed_file_roots` are supported. Durable review evidence should use `den-artifact://` refs from the artifacts service when available. `visual-inspect` resolves the ref through the artifacts metadata/content API, then still enforces its own max-byte and max-pixel limits after fetch.

Use `POST /v1/visual-inspect/describe` before review-gate evaluation when the next agent only needs a plain-language summary of the screenshot. Description packets are supporting context, not review verdicts.

Artifacts integration rules:

- canonical refs use `den-artifact://<artifact_id>`;
- scoped refs use `den-artifact://<project>/tasks/<task_id>/artifacts/<logical_name>`;
- sensitive metadata from the artifact registry is preserved during preflight logging and evaluator input;
- review packets stay ref-only;
- artifact retention and deletion semantics belong to the artifacts service.

## Relationship To visual-contract

Use `visual-contract` for deterministic, authored checks over browser evidence: DOM selectors, text, box relationships, and stable layout contracts.

Use `visual-inspect` for semantic screenshot judgment when a deterministic collector cannot prove the acceptance condition, such as:

- selected or focused card state;
- approximate visual grouping;
- screenshot-level counts;
- visual ambiguity requiring `uncertain`;
- human-review supporting evidence.

The two services can support the same review, but neither should absorb the other's authority. `visual-contract` stays deterministic. `visual-inspect` stays stateless and probabilistic.

## Verification

Recommended local checks:

```bash
GOCACHE=/home/dev/den-services/.gocache go test ./visual-inspect/...
go build ./visual-inspect/...
make test
git diff --check
```

Run the fake local evaluator smoke above after changing endpoint wiring, auth, prompt loading, schema metadata, or artifact fetching.
