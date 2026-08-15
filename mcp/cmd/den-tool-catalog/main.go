package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"den-services/mcp/internal/backend"
	"den-services/mcp/internal/registry"
)

type catalog struct {
	Revision string `json:"revision"`
	Tools    []tool `json:"tools"`
}

type tool struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	Backend      string          `json:"backend"`
	Operation    string          `json:"operation"`
	WorkflowTier string          `json:"workflow_tier"`
	Risk         string          `json:"risk"`
	InputSchema  json.RawMessage `json:"input_schema"`
}

func main() {
	output := flag.String("output", "", "output path (stdout when omitted)")
	routesPath := flag.String("routes", "mcp/routes.example.yaml", "authoritative route table path")
	flag.Parse()
	reg, err := registry.DefaultRegistry()
	if err != nil {
		fatal(err)
	}
	routes, err := backend.LoadRouteTable(*routesPath)
	if err != nil {
		fatal(err)
	}
	listed := reg.CatalogTools()
	result := catalog{Revision: registry.ToolCatalogRevision, Tools: make([]tool, 0, len(listed))}
	for _, item := range listed {
		definition, err := reg.Resolve(item.Name)
		if err != nil {
			fatal(err)
		}
		route, err := routes.Resolve(definition.Operation)
		if err != nil && !errors.Is(err, backend.ErrRouteNotFound) {
			fatal(err)
		}
		owningBackend := definition.Backend
		if err == nil {
			owningBackend = route.Backend
		}
		result.Tools = append(result.Tools, tool{
			Name: item.Name, Description: item.Description, Backend: owningBackend,
			Operation: definition.Operation, WorkflowTier: string(item.WorkflowTier),
			Risk: riskFor(definition.Operation), InputSchema: json.RawMessage(item.InputSchema),
		})
	}
	sort.Slice(result.Tools, func(i, j int) bool { return result.Tools[i].Name < result.Tools[j].Name })
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fatal(err)
	}
	data = append(data, '\n')
	if *output == "" {
		_, err = os.Stdout.Write(data)
	} else {
		err = os.WriteFile(*output, data, 0o644)
	}
	if err != nil {
		fatal(err)
	}
}

func riskFor(name string) string {
	switch name {
	case "archive_space", "delete_agent_guidance_entry", "delete_document", "den_knowledge_delete", "purge_board_comment", "purge_board_post":
		return "destructive"
	case "await_github_checks", "den_knowledge_store":
		return "write"
	}
	for _, prefix := range []string{"add_", "comment_", "create_", "ensure_", "finalize_", "mark_", "post_", "record_", "remove_", "request_", "respond_", "send_", "set_", "split_", "store_", "update_", "watch_"} {
		if strings.HasPrefix(name, prefix) {
			return "write"
		}
	}
	return "read"
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
