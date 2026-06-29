package messages

import (
	"fmt"
	"strings"
)

func renderContextPacket(task TaskContext, recent []*Message, packetType string, role string, mode string) string {
	var builder strings.Builder
	builder.WriteString("# Den Worker Context Packet\n\n")
	builder.WriteString(fmt.Sprintf("- packet_type: %s\n", packetType))
	builder.WriteString(fmt.Sprintf("- role: %s\n", role))
	builder.WriteString(fmt.Sprintf("- project_id: %s\n", task.ProjectID))
	builder.WriteString(fmt.Sprintf("- task_id: %d\n", task.ID))
	builder.WriteString(fmt.Sprintf("- title: %s\n", task.Title))
	builder.WriteString(fmt.Sprintf("- status: %s\n", task.Status))
	builder.WriteString(fmt.Sprintf("- priority: %d\n", task.Priority))
	builder.WriteString(fmt.Sprintf("- completion_reporting_mode: %s\n\n", mode))
	if strings.TrimSpace(task.Description) != "" {
		builder.WriteString("## Task Description\n\n")
		builder.WriteString(task.Description)
		builder.WriteString("\n\n")
	}
	builder.WriteString("## Recent Task Messages\n\n")
	if len(recent) == 0 {
		builder.WriteString("No recent task-thread messages were found.\n\n")
	} else {
		for _, message := range recent {
			builder.WriteString(fmt.Sprintf("- %s: %s\n", message.Sender(), preview(message.Content())))
		}
		builder.WriteString("\n")
	}
	builder.WriteString("## Safety\n\n")
	builder.WriteString("This packet is a durable instruction record. Replaying it must not create, claim, wake, retry, complete, or cancel executable work.\n")
	return builder.String()
}
