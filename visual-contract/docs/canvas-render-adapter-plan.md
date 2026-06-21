# Canvas and Render Evidence Adapter Plan

## Purpose

The next visual-contract adapter should accept deterministic canvas/render
evidence without turning `visual-contract` into generic computer vision. The
service should verify visible layout, bounds, labels, overlays, and screenshot
regions. Project clients such as ASHA Studio remain responsible for scene
authority: object IDs, transforms, camera pose, materials, selection state, and
command provenance.

## Evidence Shape

A future `RenderEvidenceRequest` should be typed and explicit:

- `scene_id`, `project`, and viewport metadata;
- screenshot or canvas crop refs, with hashes for before/after comparison;
- canvas object bounds in page/viewport coordinates;
- renderable IDs visible in the canvas;
- camera pose/projection metadata when the project can provide it;
- selected, preview, and applied renderable IDs;
- pick/hit-test evidence linking screen regions to renderable IDs;
- optional pixel samples or crop hashes for deterministic visibility checks;
- project vocabulary roles such as `central_3d_viewport`, `selection_outline`,
  `preview_ghost`, and `axis_gizmo`.

## Mapping to Visual Contracts

The adapter should emit normal `layered-visual-contract/v0.1` records:

- a root `viewport` space;
- a `canvas_region` or `central_3d_viewport` object for the canvas bounds;
- child/overlay objects for selection outlines, preview ghosts, axis gizmos,
  limitation labels, and other visible affordances;
- evidence records labeled by source, for example `render_pick`,
  `canvas_crop_hash`, `scene_readback`, or `dom_box`;
- authored constraints for visibility, containment, area ratio, and relation
  checks.

Scene semantic evidence should be referenced, not absorbed as arbitrary JSON.
For ASHA, a screenshot-visible `selection_outline` should cite scene readback
that proves which renderable is selected, but `visual-contract` should not
become the owner of that proof.

## API Boundary

Suggested route:

```text
POST /visual-contracts/from-render-evidence
```

The route should return a normal `Contract`. Compare and artifact persistence
then use the already implemented paths.

## Project Client Boundary

ASHA Studio proof scripts should:

- capture browser/DOM evidence for surrounding UI;
- capture renderer readback for scene/camera/pick data;
- compute crop hashes or pick samples near selected/preview objects;
- submit typed render evidence to `visual-contract`;
- compare the generated candidate contract against ASHA authored constraints.

`visual-contract` should not:

- infer 3D object identity from pixels alone;
- claim WebGL/Three.js output is equivalent to native/WASM runtime authority;
- require ASHA-only fields for non-ASHA consumers;
- use model-only inference as final pass/fail authority.

## Risks

- False visual claims: a screenshot may show an outline, but only scene readback
  can prove which object it represents.
- Nondeterministic model enrichment: any future OCR/vision enrichment must be
  advisory unless promoted by authored constraints or structured evidence.
- Project leakage: ASHA roles should live in vocabulary fixtures/config, not in
  generic comparator logic.
- Overfitting to crops: crop hashes prove a region changed, not that the change
  is semantically correct.

## Proposed Follow-Up Tasks

1. Define `RenderEvidenceRequest` Go DTOs and fixtures.
2. Add `/visual-contracts/from-render-evidence`.
3. Add ASHA render-readback fixture with camera, selected ID, preview ID, and
   pick evidence.
4. Add comparison tests that combine browser evidence, render evidence, and
   authored ASHA constraints.
5. Add artifact examples showing screenshot crop refs beside visual-contract
   overlays.
