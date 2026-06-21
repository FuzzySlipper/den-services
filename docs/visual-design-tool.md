# Layered Visual Contract — Design Pitch

## Goal

Build a project-aware visual decomposition and verification service for agent workflows.

The service takes a reference image, screenshot, webpage, canvas, or game render and converts it into a structured, traceable visual contract that agents can reason over. Later, agents submit a candidate screenshot or rendered state to the same service, which decomposes it into the same format and compares it against the reference contract.

The purpose is to avoid relying on vague “vision model says it looks right” evaluation. Instead, visual work becomes testable through an inspectable artifact: objects, bounds, layers, relationships, roles, constraints, and confidence.

## Core Idea

The service should not treat an image as raw pixels. It should decompose it into a **Layered Visual Contract**:

* **Objects**: text, panels, buttons, icons, cards, images, shapes, controls, overlays.
* **Bounds**: normalized coordinates plus original pixel evidence.
* **Spaces**: viewport, parent containers, panels, cards, modals, canvas regions.
* **Layers**: explicit z-order and occlusion relationships.
* **Relations**: above, below, left_of, right_of, inside, overlaps, aligned_with, attached_to, over, under.
* **Roles**: heading, primary action, warning badge, nav item, status panel, domain-specific UI concept.
* **Constraints**: design intent extracted from the visual, not just measured coordinates.
* **Evidence**: source screenshot regions, confidence, OCR text, DOM/accessibility anchors when available.

Coordinates are evidence. Constraints are intent. Roles are meaning. Layers are reality.

## Why This Exists

Agents are bad at consistently reasoning from screenshots alone. They need stable handles and explicit relationships.

Instead of telling an agent:

> “Make this look like the screenshot.”

We give it:

> “There is a primary hero section. It contains a dominant left-aligned heading, a subordinate paragraph, a primary CTA below it, and a preview card to the right. The CTA must remain below the heading, visually prominent, and above the fold.”

Then verification can produce a structured diff:

```text
FAIL preview_card_position:
  expected preview_card right_of hero_text_cluster
  found preview_card below hero_text_cluster
  severity: major

PASS primary_cta_visible:
  CTA exists, text matches, above fold

WARN title_size:
  heading exists but visual dominance is weaker than reference
```

## Recommended Architecture

```text
reference image / screenshot / webpage / render
  ↓
decomposition service
  ↓
reference.visual-contract.json
  ↓
agent implementation
  ↓
candidate screenshot / webpage / render
  ↓
same decomposition service
  ↓
candidate.visual-contract.json
  ↓
comparator
  ↓
visual diff report + annotated overlays + pass/fail constraints
```

## Inputs

The service should accept multiple evidence sources:

### Generic Image

* Screenshot/image
* Optional user prompt or design intent
* Optional project vocabulary

### Web UI

* Screenshot
* DOM snapshot
* Accessibility tree
* Computed style summary
* Element bounding boxes
* OCR fallback

### Canvas/Game/Engine Render

* Screenshot
* Optional engine scene export
* Optional ECS/entity IDs
* Camera transform
* Project asset IDs
* Project vocabulary

The service should prefer structured evidence over pure vision when available. Vision should enrich the contract, not be the only source of truth.

## Core Format

```json
{
  "schema": "layered-visual-contract/v0.1",
  "scene": {
    "id": "reference_homepage",
    "type": "web_ui",
    "viewport": {
      "width_px": 1440,
      "height_px": 900
    },
    "coordinate_mode": "normalized_with_pixel_evidence"
  },
  "project": {
    "id": "asha",
    "vocabulary": "asha_ui_v1"
  },
  "spaces": [
    {
      "id": "viewport",
      "kind": "root",
      "bounds": {
        "space": null,
        "x": 0,
        "y": 0,
        "w": 1,
        "h": 1
      }
    }
  ],
  "layers": [
    {
      "id": "base",
      "z": 0,
      "contains": ["background", "main_content"]
    },
    {
      "id": "floating",
      "z": 10,
      "contains": []
    },
    {
      "id": "modal",
      "z": 100,
      "contains": []
    }
  ],
  "objects": [
    {
      "id": "hero_section",
      "kind": "section",
      "role": "hero",
      "domain_role": null,
      "parent": "viewport",
      "layer": "base",
      "bounds": {
        "space": "viewport",
        "x": 0.06,
        "y": 0.08,
        "w": 0.88,
        "h": 0.42,
        "px": {
          "x": 86,
          "y": 72,
          "w": 1267,
          "h": 378
        }
      },
      "children": ["hero_title", "hero_subtitle", "primary_cta", "preview_card"],
      "importance": "critical",
      "confidence": 0.94
    },
    {
      "id": "hero_title",
      "kind": "text",
      "role": "heading_1",
      "parent": "hero_section",
      "layer": "base",
      "text": "Manage your city without losing your mind",
      "bounds": {
        "space": "hero_section",
        "x": 0.02,
        "y": 0.08,
        "w": 0.48,
        "h": 0.20
      },
      "text_style": {
        "role": "dominant_heading",
        "font_size_px": 48,
        "font_size_norm": 0.053,
        "weight": 700,
        "alignment": "left",
        "wrap": "2_lines"
      },
      "importance": "critical",
      "confidence": 0.96
    },
    {
      "id": "primary_cta",
      "kind": "button",
      "role": "primary_action",
      "parent": "hero_section",
      "layer": "base",
      "text": "Start planning",
      "bounds": {
        "space": "hero_section",
        "x": 0.02,
        "y": 0.62,
        "w": 0.18,
        "h": 0.12
      },
      "style": {
        "shape": "rounded_rect",
        "visual_weight": "high",
        "color_role": "accent.primary"
      },
      "behavior": {
        "click": "start_primary_flow"
      },
      "importance": "critical",
      "confidence": 0.94
    },
    {
      "id": "preview_card",
      "kind": "panel",
      "role": "visual_preview",
      "parent": "hero_section",
      "layer": "base",
      "bounds": {
        "space": "hero_section",
        "x": 0.62,
        "y": 0.04,
        "w": 0.34,
        "h": 0.72
      },
      "semantic_description": "large preview card showing city/factory visual",
      "importance": "major",
      "confidence": 0.84
    }
  ],
  "relations": [
    {
      "type": "above",
      "a": "hero_title",
      "b": "primary_cta",
      "confidence": 0.98
    },
    {
      "type": "right_of",
      "a": "preview_card",
      "b": "hero_title",
      "confidence": 0.94
    },
    {
      "type": "aligned_left",
      "items": ["hero_title", "primary_cta"],
      "confidence": 0.91
    }
  ],
  "constraints": [
    {
      "id": "hero_title_above_cta",
      "type": "relative_position",
      "a": "hero_title",
      "relation": "above",
      "b": "primary_cta",
      "importance": "critical",
      "tolerance": "medium"
    },
    {
      "id": "hero_cluster_left_aligned",
      "type": "alignment",
      "items": ["hero_title", "primary_cta"],
      "edge": "left",
      "importance": "major",
      "tolerance_norm": 0.02
    },
    {
      "id": "preview_card_right_of_hero_text",
      "type": "layout_relation",
      "a": "preview_card",
      "relation": "right_of",
      "b": "hero_title",
      "importance": "major"
    }
  ],
  "evidence": {
    "source_type": "screenshot",
    "source_ref": "reference.png",
    "generated_by": "visual-contract-service",
    "overall_confidence": 0.91
  }
}
```

## Relationship Vocabulary

Use consistent terms so agents do not confuse spatial and layer relationships.

### Spatial

* `left_of`
* `right_of`
* `above`
* `below`
* `near`
* `far_from`
* `centered_in`
* `aligned_left`
* `aligned_right`
* `aligned_top`
* `aligned_bottom`
* `same_size_as`

### Containment

* `inside`
* `contains`
* `partially_inside`
* `escapes_parent`
* `clipped_by`

### Layering

* `over`
* `under`
* `occludes`
* `occluded_by`
* `translucent_over`

### Attachment

* `anchored_to`
* `docked_to_edge`
* `badge_on_corner`
* `tooltip_for`
* `label_for`

### Flow

* `precedes`
* `follows`
* `grouped_with`
* `repeated_in_list`
* `part_of_grid`

### Visual Hierarchy

* `dominant_over`
* `subordinate_to`
* `same_weight_as`
* `deemphasized_relative_to`

## Comparator Output

The comparator should produce a traceable report, not just a score.

```json
{
  "score": 0.82,
  "verdict": "needs_revision",
  "passes": [
    {
      "constraint": "hero_title_above_cta",
      "message": "hero_title is above primary_cta within tolerance"
    }
  ],
  "failures": [
    {
      "severity": "major",
      "constraint": "preview_card_right_of_hero_text",
      "expected": "preview_card right_of hero_title",
      "actual": "preview_card below hero_title",
      "evidence": {
        "reference_object": "preview_card",
        "candidate_object": "preview_card"
      }
    }
  ],
  "warnings": [
    {
      "severity": "minor",
      "constraint": "hero_visual_hierarchy",
      "message": "hero_title exists but appears less dominant than expected"
    }
  ],
  "artifacts": {
    "reference_overlay": "reference.overlay.svg",
    "candidate_overlay": "candidate.overlay.svg",
    "diff_overlay": "diff.overlay.svg"
  }
}
```

## MVP Scope

Start with web UI and static screenshots.

1. Accept screenshot plus optional DOM/accessibility/style metadata.
2. Detect text, panels, controls, images, icons, and obvious groups.
3. Emit normalized bounds with pixel evidence.
4. Infer common relationships: containment, above/below, left/right, alignment, grouping, visual dominance.
5. Generate an annotated SVG overlay for human debugging.
6. Compare reference and candidate contracts.
7. Emit structured pass/fail/warn report.
8. Allow project-specific vocabulary and roles to be injected.

Do not start by trying to solve all visual understanding. The valuable custom layer is the contract schema, project vocabulary, normalizer, and comparator.

## Non-Goals for MVP

* Pixel-perfect visual regression.
* Fully generic image understanding.
* Perfect design intent inference.
* Replacing human design judgment.
* Building a full Figma clone.
* Making vision models the final judge.

## Main Design Principle

The service should turn visual work into something agents can plan against and test against.

Not:

> “Does this screenshot look right?”

But:

> “Does this candidate satisfy the extracted visual contract?”
