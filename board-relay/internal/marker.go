package boardrelay

import (
	"fmt"
	"strconv"
	"strings"
)

type relayMarker struct {
	ProjectID       string
	BoardPostID     int64
	BoardCommentID  int64
	ParentCommentID int64
}

func postMarker(projectID string, postID int64) string {
	return fmt.Sprintf("%s project=%q board-post=%d -->", markerPrefix, projectID, postID)
}

func commentMarker(projectID string, commentID int64, parentCommentID *int64) string {
	parent := ""
	if parentCommentID != nil {
		parent = fmt.Sprintf(" parent-comment=%d", *parentCommentID)
	}
	return fmt.Sprintf("%s project=%q board-comment=%d%s -->", markerPrefix, projectID, commentID, parent)
}

func parseMarker(body string) (relayMarker, bool) {
	start := strings.Index(body, markerPrefix)
	if start < 0 {
		return relayMarker{}, false
	}
	end := strings.Index(body[start:], "-->")
	if end < 0 {
		return relayMarker{}, false
	}
	fields := strings.Fields(strings.TrimSpace(strings.TrimSuffix(body[start:start+end+3], "-->")))
	marker := relayMarker{}
	for _, field := range fields[2:] {
		parts := strings.SplitN(field, "=", 2)
		if len(parts) != 2 {
			continue
		}
		value := strings.Trim(parts[1], "\"")
		switch parts[0] {
		case "project":
			marker.ProjectID = value
		case "board-post":
			marker.BoardPostID, _ = strconv.ParseInt(value, 10, 64)
		case "board-comment":
			marker.BoardCommentID, _ = strconv.ParseInt(value, 10, 64)
		case "parent-comment":
			marker.ParentCommentID, _ = strconv.ParseInt(value, 10, 64)
		}
	}
	return marker, marker.ProjectID != ""
}
