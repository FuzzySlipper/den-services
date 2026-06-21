package visualcontract

import (
	"fmt"
	"html"
	"sort"
	"strings"
)

func RenderContractOverlay(contract *Contract, report *ComparisonReport) (string, error) {
	if err := ValidateContract(contract); err != nil {
		return "", err
	}
	highlight := failedObjectRefs(report)
	var builder strings.Builder
	writeSVGHeader(&builder, contract.Scene.Viewport)
	writeOverlayObjects(&builder, contract.Objects, contract.Scene.Viewport, highlight)
	writeSVGFooter(&builder)
	return builder.String(), nil
}

func RenderDiffOverlay(candidate *Contract, report *ComparisonReport) (string, error) {
	if err := ValidateContract(candidate); err != nil {
		return "", err
	}
	var builder strings.Builder
	writeSVGHeader(&builder, candidate.Scene.Viewport)
	writeOverlayObjects(&builder, candidate.Objects, candidate.Scene.Viewport, failedObjectRefs(report))
	writeFailures(&builder, report)
	writeSVGFooter(&builder)
	return builder.String(), nil
}

func writeSVGHeader(builder *strings.Builder, viewport Viewport) {
	builder.WriteString(fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" width="%d" height="%d" role="img">`, viewport.WidthPX, viewport.HeightPX, viewport.WidthPX, viewport.HeightPX))
	builder.WriteString(`<rect x="0" y="0" width="100%" height="100%" fill="rgba(255,255,255,0.01)"/>`)
}

func writeSVGFooter(builder *strings.Builder) {
	builder.WriteString(`</svg>`)
}

func writeOverlayObjects(builder *strings.Builder, objects []Object, viewport Viewport, highlight map[string]Importance) {
	sorted := append([]Object(nil), objects...)
	sort.Slice(sorted, func(i int, j int) bool {
		return sorted[i].ID < sorted[j].ID
	})
	for _, object := range sorted {
		x := object.Bounds.X * float64(viewport.WidthPX)
		y := object.Bounds.Y * float64(viewport.HeightPX)
		w := object.Bounds.W * float64(viewport.WidthPX)
		h := object.Bounds.H * float64(viewport.HeightPX)
		color := "#2563eb"
		strokeWidth := 2
		if severity, ok := highlight[object.ID]; ok {
			color = severityColor(severity)
			strokeWidth = 4
		}
		label := html.EscapeString(object.ID + " · " + object.Role)
		builder.WriteString(fmt.Sprintf(`<rect data-object-id="%s" x="%.2f" y="%.2f" width="%.2f" height="%.2f" fill="none" stroke="%s" stroke-width="%d"/>`, html.EscapeString(object.ID), x, y, w, h, color, strokeWidth))
		builder.WriteString(fmt.Sprintf(`<text data-label-for="%s" x="%.2f" y="%.2f" font-size="14" fill="%s">%s</text>`, html.EscapeString(object.ID), x+4, y+16, color, label))
	}
}

func writeFailures(builder *strings.Builder, report *ComparisonReport) {
	if report == nil || len(report.Failures) == 0 {
		return
	}
	y := 24.0
	builder.WriteString(`<g data-overlay-section="failures">`)
	for _, failure := range report.Failures {
		builder.WriteString(fmt.Sprintf(`<text x="16" y="%.2f" font-size="16" fill="%s">%s: %s</text>`, y, severityColor(failure.Severity), html.EscapeString(failure.Constraint), html.EscapeString(failure.Message)))
		y += 22
	}
	builder.WriteString(`</g>`)
}

func failedObjectRefs(report *ComparisonReport) map[string]Importance {
	refs := make(map[string]Importance)
	if report == nil {
		return refs
	}
	for _, result := range append(append([]CheckResult(nil), report.Failures...), report.Warnings...) {
		for _, key := range []string{"a", "b", "object"} {
			value := result.Evidence[key]
			if value != "" {
				refs[value] = result.Severity
			}
		}
	}
	return refs
}

func severityColor(importance Importance) string {
	switch importance {
	case ImportanceCritical:
		return "#dc2626"
	case ImportanceMajor:
		return "#ea580c"
	case ImportanceMinor:
		return "#d97706"
	default:
		return "#16a34a"
	}
}
