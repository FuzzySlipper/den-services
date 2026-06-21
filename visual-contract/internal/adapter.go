package visualcontract

import (
	"fmt"
	"sort"
	"strings"
)

type WebEvidence struct {
	SceneID       string       `json:"scene_id"`
	Project       *Project     `json:"project,omitempty"`
	Viewport      Viewport     `json:"viewport"`
	ScreenshotRef string       `json:"screenshot_ref,omitempty"`
	Nodes         []WebNode    `json:"nodes"`
	Constraints   []Constraint `json:"constraints,omitempty"`
}

type WebNode struct {
	ID             string            `json:"id"`
	ParentID       string            `json:"parent_id,omitempty"`
	TestID         string            `json:"test_id,omitempty"`
	Role           string            `json:"role,omitempty"`
	AccessibleName string            `json:"accessible_name,omitempty"`
	Text           string            `json:"text,omitempty"`
	Tag            string            `json:"tag,omitempty"`
	BoundsPX       PixelBounds       `json:"bounds_px"`
	Styles         WebStyleSummary   `json:"styles,omitempty"`
	Attributes     map[string]string `json:"attributes,omitempty"`
}

type WebStyleSummary struct {
	Display    string   `json:"display,omitempty"`
	Position   string   `json:"position,omitempty"`
	ZIndex     *int     `json:"z_index,omitempty"`
	FontSizePX *int     `json:"font_size_px,omitempty"`
	FontWeight string   `json:"font_weight,omitempty"`
	Background string   `json:"background,omitempty"`
	Color      string   `json:"color,omitempty"`
	Opacity    *float64 `json:"opacity,omitempty"`
}

func BuildContractFromWebEvidence(evidence *WebEvidence) (*Contract, error) {
	if evidence == nil {
		return nil, invalidRequest("web evidence is required")
	}
	if evidence.SceneID == "" {
		return nil, invalidRequest("scene_id is required")
	}
	if evidence.Viewport.WidthPX <= 0 || evidence.Viewport.HeightPX <= 0 {
		return nil, invalidRequest("viewport dimensions must be positive")
	}
	if len(evidence.Nodes) == 0 {
		return nil, invalidRequest("at least one web node is required")
	}

	layers := buildWebLayers(evidence.Nodes)
	objects := make([]Object, 0, len(evidence.Nodes))
	records := make([]EvidenceRecord, 0, len(evidence.Nodes))
	for _, node := range evidence.Nodes {
		objectID := webObjectID(node)
		parent := "viewport"
		if node.ParentID != "" {
			parent = webObjectIDByNodeID(evidence.Nodes, node.ParentID)
		}
		layer := webLayerID(node)
		records = append(records, EvidenceRecord{
			ID:         "web_node:" + objectID,
			Kind:       "dom_accessibility_box_style",
			SourceRef:  node.ID,
			ObjectRefs: []string{objectID},
			Confidence: webConfidence(node),
		})
		objects = append(objects, Object{
			ID:           objectID,
			Kind:         webKind(node),
			Role:         webRole(node),
			Parent:       parent,
			Layer:        layer,
			Text:         firstNonEmpty(node.AccessibleName, node.Text),
			Bounds:       normalizeViewportBounds(node.BoundsPX, evidence.Viewport),
			Importance:   ImportanceMajor,
			Confidence:   webConfidence(node),
			EvidenceRefs: []string{"web_node:" + objectID},
			Style:        webStyleMap(node.Styles),
		})
	}
	return &Contract{
		Schema: SchemaVersion,
		Scene: Scene{
			ID:             evidence.SceneID,
			Type:           "web_ui",
			Viewport:       evidence.Viewport,
			CoordinateMode: "normalized_with_pixel_evidence",
		},
		Project: evidence.Project,
		Spaces: []Space{
			{
				ID:   "viewport",
				Kind: "root",
				Bounds: Bounds{
					X: 0,
					Y: 0,
					W: 1,
					H: 1,
				},
			},
		},
		Layers:      layers,
		Objects:     objects,
		Relations:   inferRelations(objects),
		Constraints: evidence.Constraints,
		Evidence: EvidenceSet{
			SourceType:        "web_evidence",
			SourceRef:         evidence.ScreenshotRef,
			GeneratedBy:       "visual-contract-service",
			OverallConfidence: averageEvidenceConfidence(records),
			Records:           records,
		},
	}, nil
}

func buildWebLayers(nodes []WebNode) []Layer {
	layerObjects := make(map[string][]string)
	for _, node := range nodes {
		layerObjects[webLayerID(node)] = append(layerObjects[webLayerID(node)], webObjectID(node))
	}
	ids := make([]string, 0, len(layerObjects))
	for id := range layerObjects {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	layers := make([]Layer, 0, len(ids))
	for _, id := range ids {
		z := 0
		if strings.HasPrefix(id, "z_") {
			_, _ = fmt.Sscanf(id, "z_%d", &z)
		}
		sort.Strings(layerObjects[id])
		layers = append(layers, Layer{ID: id, Z: z, Contains: layerObjects[id]})
	}
	return layers
}

func inferRelations(objects []Object) []Relation {
	relations := make([]Relation, 0)
	for i, a := range objects {
		for _, b := range objects[i+1:] {
			relations = appendDirectionalRelations(relations, a, b)
			if absFloat(a.Bounds.X-b.Bounds.X) <= 0.02 {
				relations = append(relations, Relation{Type: RelationAlignedLeft, Items: []string{a.ID, b.ID}, Confidence: 0.82})
			}
		}
	}
	return relations
}

func appendDirectionalRelations(relations []Relation, a Object, b Object) []Relation {
	for _, candidate := range []struct {
		relation RelationType
		first    Object
		second   Object
	}{
		{RelationInside, a, b},
		{RelationInside, b, a},
		{RelationContains, a, b},
		{RelationContains, b, a},
		{RelationRightOf, a, b},
		{RelationRightOf, b, a},
		{RelationLeftOf, a, b},
		{RelationLeftOf, b, a},
		{RelationBelow, a, b},
		{RelationBelow, b, a},
		{RelationAbove, a, b},
		{RelationAbove, b, a},
	} {
		if relationHolds(candidate.first.Bounds, candidate.second.Bounds, candidate.relation, 0.02) {
			relations = append(relations, Relation{
				Type:       candidate.relation,
				A:          candidate.first.ID,
				B:          candidate.second.ID,
				Confidence: relationConfidence(candidate.relation),
			})
		}
	}
	return relations
}

func relationConfidence(relation RelationType) float64 {
	switch relation {
	case RelationInside, RelationContains:
		return 0.9
	default:
		return 0.84
	}
}

func webObjectID(node WebNode) string {
	if node.TestID != "" {
		return cleanID(node.TestID)
	}
	if value := node.Attributes["data-visual-id"]; value != "" {
		return cleanID(value)
	}
	if value := node.Attributes["id"]; value != "" {
		return cleanID(value)
	}
	return cleanID(node.ID)
}

func webObjectIDByNodeID(nodes []WebNode, id string) string {
	for _, node := range nodes {
		if node.ID == id {
			return webObjectID(node)
		}
	}
	return "viewport"
}

func webRole(node WebNode) string {
	if value := node.Attributes["data-visual-role"]; value != "" {
		return value
	}
	if node.Role != "" {
		return node.Role
	}
	if node.Tag != "" {
		return node.Tag
	}
	return "generic"
}

func webKind(node WebNode) string {
	switch strings.ToLower(node.Tag) {
	case "button":
		return "button"
	case "img", "svg", "canvas":
		return "image"
	case "h1", "h2", "h3", "p", "span", "label":
		return "text"
	case "section", "main", "article", "aside", "nav", "div":
		return "panel"
	default:
		return "element"
	}
}

func webLayerID(node WebNode) string {
	if node.Styles.ZIndex == nil {
		return "z_0"
	}
	return fmt.Sprintf("z_%d", *node.Styles.ZIndex)
}

func webConfidence(node WebNode) float64 {
	switch {
	case node.TestID != "":
		return 0.98
	case node.Attributes["data-visual-id"] != "":
		return 0.96
	case node.Role != "" && node.AccessibleName != "":
		return 0.9
	default:
		return 0.76
	}
}

func normalizeViewportBounds(bounds PixelBounds, viewport Viewport) Bounds {
	return Bounds{
		Space: "viewport",
		X:     float64(bounds.X) / float64(viewport.WidthPX),
		Y:     float64(bounds.Y) / float64(viewport.HeightPX),
		W:     float64(bounds.W) / float64(viewport.WidthPX),
		H:     float64(bounds.H) / float64(viewport.HeightPX),
		PX:    &bounds,
	}
}

func webStyleMap(style WebStyleSummary) map[string]string {
	values := map[string]string{}
	if style.Display != "" {
		values["display"] = style.Display
	}
	if style.Position != "" {
		values["position"] = style.Position
	}
	if style.FontWeight != "" {
		values["font_weight"] = style.FontWeight
	}
	if style.Background != "" {
		values["background"] = style.Background
	}
	if style.Color != "" {
		values["color"] = style.Color
	}
	if style.FontSizePX != nil {
		values["font_size_px"] = fmt.Sprintf("%d", *style.FontSizePX)
	}
	if style.Opacity != nil {
		values["opacity"] = fmt.Sprintf("%.2f", *style.Opacity)
	}
	if len(values) == 0 {
		return nil
	}
	return values
}

func averageEvidenceConfidence(records []EvidenceRecord) float64 {
	if len(records) == 0 {
		return 0
	}
	total := 0.0
	for _, record := range records {
		total += record.Confidence
	}
	return total / float64(len(records))
}

func cleanID(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.ReplaceAll(value, " ", "_")
	value = strings.ReplaceAll(value, "-", "_")
	value = strings.ReplaceAll(value, ":", "_")
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func absFloat(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}
