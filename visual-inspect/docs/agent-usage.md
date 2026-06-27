# Visual Inspect Agent Usage

`visual-inspect` accepts screenshot artifact references, runs visual criteria checks, and returns a compact review evidence packet. Agents must not embed raw screenshot bytes or base64 image payloads in Den review messages; post artifact refs plus the JSON result.

## Evaluate Endpoint

Start the service with `visual-inspect/config.example.yaml` or an equivalent config. Protected API routes require a bearer token unless `security.allow_unauthenticated_local_dev` is explicitly set to `true` for local-only smoke work.

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
  --data @visual-inspect/examples/agora-terminal-selected.request.json
```

For plain image descriptions without pass/fail scoring:

```bash
curl -sS http://127.0.0.1:18140/v1/visual-inspect/describe \
  -H 'Authorization: Bearer local-review-token' \
  -H 'Content-Type: application/json' \
  --data @visual-inspect/examples/agora-describe.request.json
```

Minimal Go client:

```go
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

func main() {
	body, err := os.ReadFile("visual-inspect/examples/agora-terminal-selected.request.json")
	if err != nil {
		panic(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://127.0.0.1:18140/v1/visual-inspect/evaluate", bytes.NewReader(body))
	if err != nil {
		panic(err)
	}
	req.Header.Set("Authorization", "Bearer local-review-token")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	var packet map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&packet); err != nil {
		panic(err)
	}
	fmt.Printf("status=%d verdict=%v confidence=%v\n", resp.StatusCode, packet["verdict"], packet["confidence"])
}
```

## Request Shape

Use stable criterion and screenshot IDs. `screenshots[].ref` should be a `den-artifact://`, `file://`, `http://`, or `https://` reference allowed by service config. Prefer `den-artifact://` for durable review evidence. Keep `screenshots[].sensitive` accurate so logs and downstream packets can preserve handling intent.

```json
{
  "request_id": "task-3317-terminal-selected",
  "task_ref": {
    "project_id": "den-services",
    "task_id": 3317
  },
  "criteria": [
    {
      "id": "terminal-selected",
      "statement": "The terminal card is visibly selected or focused.",
      "required": true,
      "weight": 1
    }
  ],
  "screenshots": [
    {
      "id": "agora-overview",
      "ref": "den-artifact://art_01jexample",
      "mime_type": "image/png",
      "description": "Agora overview after selecting the terminal card"
    }
  ],
  "context": {
    "task_title": "Agora terminal focus visual check",
    "acceptance_summary": "Terminal should be selected or focused in the overview.",
    "ui_surface": "Agora overview"
  },
  "options": {
    "profile": "visual-inspect-v0",
    "min_confidence_for_pass": 0.7,
    "return_regions": true
  }
}
```

## Response Packet

Treat the response body as the visual-inspect review result. It includes the top-level verdict, confidence, per-criterion results, observations, optional pixel regions, follow-up hints, warnings, and model metadata.

```json
{
  "request_id": "task-3317-terminal-selected",
  "verdict": "pass",
  "confidence": 0.86,
  "criteria_results": [
    {
      "criterion_id": "terminal-selected",
      "verdict": "pass",
      "confidence": 0.86,
      "explanation": "The terminal card has a visible selected state.",
      "observations": [
        {
          "screenshot_id": "agora-overview",
          "label": "terminal focused card",
          "region": {
            "x": 142,
            "y": 96,
            "width": 480,
            "height": 320,
            "coordinate_space": "image_pixels"
          },
          "confidence": 0.82
        }
      ]
    }
  ],
  "follow_up_hints": [],
  "model_info": {
    "provider": "openai_compatible",
    "model": "qwen-vl-or-other",
    "prompt_profile": "visual-inspect-v0",
    "schema_version": "visual-inspect-evaluate-response/v0"
  },
  "warnings": []
}
```

## Description Packet

Use `POST /v1/visual-inspect/describe` when the caller needs a visual summary rather than a review verdict. The response is intentionally non-scoring:

```json
{
  "request_id": "task-3317-describe-overview",
  "description": "The screenshot shows an overview with browser, terminal, and notes cards. The terminal card appears selected.",
  "screenshot_ids": ["agora-overview"],
  "model_info": {
    "provider": "openai_compatible",
    "model": "Gemma-4-26B-A4B-it-GGUF",
    "prompt_profile": "visual-inspect-v0/describe"
  },
  "warnings": []
}
```

Description packets are useful context for agents or models that cannot inspect images directly. They should not be used as pass/fail review evidence unless converted into explicit criteria and checked through `/evaluate`.

## Den Review Message

When posting to Den, attach a review packet that references screenshots and embeds only the result JSON. Do not include raw PNG/JPEG bytes, data URLs, or base64 image fields in the message body or metadata.

```json
{
  "packet_type": "visual_inspect_result",
  "schema_version": "visual-inspect-review-packet/v0",
  "source": {
    "service": "visual-inspect",
    "endpoint": "POST /v1/visual-inspect/evaluate"
  },
  "artifact_refs": [
    {
      "screenshot_id": "agora-overview",
      "ref": "den-artifact://art_01jexample",
      "mime_type": "image/png",
      "sensitive": false
    }
  ],
  "result": {
    "request_id": "task-3317-terminal-selected",
    "verdict": "pass",
    "confidence": 0.86,
    "criteria_results": [],
    "follow_up_hints": [],
    "model_info": {
      "provider": "openai_compatible",
      "model": "qwen-vl-or-other",
      "prompt_profile": "visual-inspect-v0",
      "schema_version": "visual-inspect-evaluate-response/v0"
    },
    "warnings": []
  }
}
```

A concise Den review note can then say:

```text
Visual inspection: pass at 0.86 confidence. Evidence packet attached as metadata. Screenshot artifact refs are retained separately; no raw screenshot bytes are embedded in this review message.
```

## Agora Examples

`visual-inspect/examples/agora-terminal-selected.request.json` checks that the terminal card is visibly selected or focused.

`visual-inspect/examples/agora-three-overview-cards.request.json` checks that the overview shows exactly three cards: browser, terminal, and notes.

`visual-inspect/examples/agora-describe.request.json` asks for a plain-language overview description without scoring.
