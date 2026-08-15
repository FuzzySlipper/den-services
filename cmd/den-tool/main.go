package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

const toolVersion = "1.1.2"

type catalogReadback struct {
	Version string         `json:"version"`
	Tools   []toolReadback `json:"tools"`
}

type toolReadback struct {
	Tool
	Availability string `json:"availability"`
	Reason       string `json:"reason"`
}

func main() {
	os.Exit(runCLI(os.Args[1:], os.Stdout, os.Stderr))
}

func runCLI(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "--version" {
		_, _ = fmt.Fprintf(stdout, "den-tool version %s\n", toolVersion)
		return 0
	}
	if len(args) == 0 {
		writeUsageError(stderr, "a command is required")
		return 2
	}

	switch args[0] {
	case "list":
		return handleList(args[1:], stdout, stderr)
	case "search":
		return handleSearch(args[1:], stdout, stderr)
	case "describe":
		return handleDescribe(args[1:], stdout, stderr)
	case "run":
		return handleRun(args[1:], stdout, stderr)
	case "board":
		return handleBoard(args[1:], stdout, stderr)
	case "den":
		return handleDen(args[1:], stdout, stderr)
	default:
		writeUsageError(stderr, fmt.Sprintf("unknown command %q", args[0]))
		return 2
	}
}

func handleList(args []string, stdout, stderr io.Writer) int {
	remaining, jsonOutput, err := takeJSONFlag(args)
	if err != nil {
		writeUsageError(stderr, err.Error())
		return 2
	}
	if len(remaining) != 0 {
		writeUsageError(stderr, "list accepts only --json")
		return 2
	}
	catalog, err := cliCatalog()
	if err != nil {
		return writeRuntimeError(stderr, err)
	}
	if jsonOutput {
		return writeJSON(stdout, newCatalogReadback(catalog))
	}

	for _, tool := range catalog.Tools {
		_, _ = fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", tool.ID, tool.Risk, availabilityText(newToolReadback(tool)), tool.Description)
	}
	return 0
}

func handleSearch(args []string, stdout, stderr io.Writer) int {
	terms, jsonOutput, err := takeJSONFlag(args)
	if err != nil {
		writeUsageError(stderr, err.Error())
		return 2
	}
	if len(terms) == 0 {
		writeUsageError(stderr, "search requires at least one metadata term")
		return 2
	}
	catalog, err := cliCatalog()
	if err != nil {
		return writeRuntimeError(stderr, err)
	}
	result := catalog.Search(terms)
	if jsonOutput {
		return writeJSON(stdout, newCatalogReadback(result))
	}
	for _, tool := range result.Tools {
		_, _ = fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", tool.ID, tool.Risk, availabilityText(newToolReadback(tool)), tool.Description)
	}
	return 0
}

func handleDescribe(args []string, stdout, stderr io.Writer) int {
	remaining, jsonOutput, err := takeJSONFlag(args)
	if err != nil {
		writeUsageError(stderr, err.Error())
		return 2
	}
	if len(remaining) != 1 {
		writeUsageError(stderr, "describe requires exactly one tool id")
		return 2
	}
	catalog, err := cliCatalog()
	if err != nil {
		return writeRuntimeError(stderr, err)
	}
	tool, found := catalog.Find(remaining[0])
	if !found {
		return writeRuntimeError(stderr, fmt.Errorf("tool %q is not in the catalog", remaining[0]))
	}
	if jsonOutput {
		return writeJSON(stdout, newToolReadback(tool))
	}

	readback := newToolReadback(tool)
	_, _ = fmt.Fprintf(stdout, "id: %s\n", tool.ID)
	_, _ = fmt.Fprintf(stdout, "description: %s\n", tool.Description)
	_, _ = fmt.Fprintf(stdout, "tags: %s\n", strings.Join(tool.Tags, ", "))
	_, _ = fmt.Fprintf(stdout, "repo: %s\n", tool.Repo)
	_, _ = fmt.Fprintf(stdout, "root: %s\n", tool.Root)
	_, _ = fmt.Fprintf(stdout, "working directory: %s\n", tool.WorkingDirectory)
	_, _ = fmt.Fprintf(stdout, "argv: %s\n", strings.Join(tool.Argv, " "))
	_, _ = fmt.Fprintf(stdout, "risk: %s\n", tool.Risk)
	_, _ = fmt.Fprintf(stdout, "source: %s\n", tool.Source)
	if tool.Operation != "" {
		_, _ = fmt.Fprintf(stdout, "backend: %s\n", tool.Backend)
		_, _ = fmt.Fprintf(stdout, "operation: %s\n", tool.Operation)
		_, _ = fmt.Fprintf(stdout, "workflow tier: %s\n", tool.WorkflowTier)
		_, _ = fmt.Fprintf(stdout, "input schema: %s\n", string(tool.InputSchema))
	}
	_, _ = fmt.Fprintf(stdout, "requirements: %s\n", strings.Join(tool.Requirements, ", "))
	_, _ = fmt.Fprintln(stdout, "examples:")
	for _, example := range tool.Examples {
		_, _ = fmt.Fprintf(stdout, "  %s\n", example)
	}
	if readback.Availability == "available" {
		_, _ = fmt.Fprintln(stdout, "availability: available")
	} else {
		_, _ = fmt.Fprintln(stdout, "availability: unavailable")
		_, _ = fmt.Fprintf(stdout, "reason: %s\n", readback.Reason)
	}
	return 0
}

func newCatalogReadback(catalog Catalog) catalogReadback {
	result := catalogReadback{Version: catalog.Version, Tools: make([]toolReadback, 0, len(catalog.Tools))}
	for _, tool := range catalog.Tools {
		result.Tools = append(result.Tools, newToolReadback(tool))
	}
	return result
}

func newToolReadback(tool Tool) toolReadback {
	availability := CheckAvailability(tool)
	if availability.Available {
		return toolReadback{Tool: tool, Availability: "available"}
	}
	return toolReadback{Tool: tool, Availability: "unavailable", Reason: availability.Reason}
}

func availabilityText(readback toolReadback) string {
	if readback.Availability == "available" {
		return "available"
	}
	return "unavailable: " + readback.Reason
}

func handleRun(args []string, stdout, stderr io.Writer) int {
	if len(args) < 2 {
		writeUsageError(stderr, "run requires a tool id followed by -- and optional extra arguments")
		return 2
	}
	if args[1] != "--" {
		writeUsageError(stderr, "run requires -- before extra arguments")
		return 2
	}
	catalog, err := cliCatalog()
	if err != nil {
		return writeRuntimeError(stderr, err)
	}
	tool, found := catalog.Find(args[0])
	if !found {
		return writeRuntimeError(stderr, fmt.Errorf("tool %q is not in the catalog", args[0]))
	}
	if err := RunTool(context.Background(), tool, stdout, stderr, args[2:]...); err != nil {
		var pathError *os.PathError
		if errors.As(err, &pathError) {
			return writeRuntimeError(stderr, err)
		}
		var processError *exec.ExitError
		if errors.As(err, &processError) {
			code := processError.ExitCode()
			if code < 0 {
				code = 1
			}
			_, _ = fmt.Fprintf(stderr, "den-tool: %s exited with status %d\n", tool.ID, code)
			return code
		}
		return writeRuntimeError(stderr, err)
	}
	return 0
}

func handleBoard(args []string, stdout, stderr io.Writer) int {
	return runBoardCommand(args, stdout, stderr)
}

func cliCatalog() (Catalog, error) {
	repoRoot, err := RepositoryRootFromEnv()
	if err != nil {
		return Catalog{}, err
	}
	return LoadEmbeddedCatalog(repoRoot)
}

func takeJSONFlag(args []string) ([]string, bool, error) {
	remaining := make([]string, 0, len(args))
	jsonOutput := false
	for _, argument := range args {
		if argument == "--json" {
			if jsonOutput {
				return nil, false, fmt.Errorf("--json may only be provided once")
			}
			jsonOutput = true
			continue
		}
		remaining = append(remaining, argument)
	}
	return remaining, jsonOutput, nil
}

func writeJSON(writer io.Writer, value any) int {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return 1
	}
	return 0
}

func writeHumanServiceJSON(writer io.Writer, body []byte) int {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return 0
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, trimmed); err == nil {
		_, _ = fmt.Fprintln(writer, compact.String())
		return 0
	}
	_, _ = fmt.Fprintln(writer, string(trimmed))
	return 0
}

func writeUsageError(writer io.Writer, message string) {
	_, _ = fmt.Fprintf(writer, "den-tool: %s\nusage: den-tool list [--json] | search <terms...> [--json] | describe <id> [--json] | run <id> -- [extra args...] | board <subcommand> [flags] | den <operation> [flags]\n", message)
}

func writeRuntimeError(writer io.Writer, err error) int {
	_, _ = fmt.Fprintf(writer, "den-tool: %v\n", err)
	return 1
}
