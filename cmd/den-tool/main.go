package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

const toolVersion = "1.0.0"

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
		fmt.Fprintf(stdout, "den-tool version %s\n", toolVersion)
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
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", tool.ID, tool.Risk, availabilityText(newToolReadback(tool)), tool.Description)
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
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", tool.ID, tool.Risk, availabilityText(newToolReadback(tool)), tool.Description)
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
	fmt.Fprintf(stdout, "id: %s\n", tool.ID)
	fmt.Fprintf(stdout, "description: %s\n", tool.Description)
	fmt.Fprintf(stdout, "tags: %s\n", strings.Join(tool.Tags, ", "))
	fmt.Fprintf(stdout, "repo: %s\n", tool.Repo)
	fmt.Fprintf(stdout, "root: %s\n", tool.Root)
	fmt.Fprintf(stdout, "working directory: %s\n", tool.WorkingDirectory)
	fmt.Fprintf(stdout, "argv: %s\n", strings.Join(tool.Argv, " "))
	fmt.Fprintf(stdout, "risk: %s\n", tool.Risk)
	fmt.Fprintf(stdout, "source: %s\n", tool.Source)
	fmt.Fprintf(stdout, "requirements: %s\n", strings.Join(tool.Requirements, ", "))
	fmt.Fprintln(stdout, "examples:")
	for _, example := range tool.Examples {
		fmt.Fprintf(stdout, "  %s\n", example)
	}
	if readback.Availability == "available" {
		fmt.Fprintln(stdout, "availability: available")
	} else {
		fmt.Fprintln(stdout, "availability: unavailable")
		fmt.Fprintf(stdout, "reason: %s\n", readback.Reason)
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
			fmt.Fprintf(stderr, "den-tool: %s exited with status %d\n", tool.ID, code)
			return code
		}
		return writeRuntimeError(stderr, err)
	}
	return 0
}

func handleBoard(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "search" {
		writeUsageError(stderr, "board requires the search subcommand")
		return 2
	}

	flags := flag.NewFlagSet("board search", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	project := flags.String("project", "", "Board project id")
	query := flags.String("query", "", "Board search query")
	afterID := flags.Int64("after-id", -1, "Optional Board cursor")
	limit := flags.Int("limit", -1, "Optional result limit")
	jsonOutput := flags.Bool("json", false, "Print service JSON")
	if err := flags.Parse(args[1:]); err != nil {
		writeUsageError(stderr, fmt.Sprintf("invalid board search flags: %v", err))
		return 2
	}
	if flags.NArg() != 0 {
		writeUsageError(stderr, "board search does not accept positional arguments")
		return 2
	}
	if strings.TrimSpace(*project) == "" {
		writeUsageError(stderr, "board search requires --project")
		return 2
	}
	if strings.TrimSpace(*query) == "" {
		writeUsageError(stderr, "board search requires --query")
		return 2
	}
	if *afterID < -1 {
		writeUsageError(stderr, "board search --after-id must be non-negative")
		return 2
	}
	if *limit == 0 || *limit < -1 {
		writeUsageError(stderr, "board search --limit must be positive")
		return 2
	}

	options := BoardSearchOptions{ProjectID: *project, Query: *query}
	if *afterID >= 0 {
		options.AfterID = afterID
	}
	if *limit > 0 {
		options.Limit = limit
	}
	client, err := BoardClientFromEnv()
	if err != nil {
		return writeRuntimeError(stderr, err)
	}
	body, err := client.Search(context.Background(), options)
	if err != nil {
		return writeRuntimeError(stderr, err)
	}
	if *jsonOutput {
		if _, err := stdout.Write(body); err != nil {
			return writeRuntimeError(stderr, fmt.Errorf("write board response: %w", err))
		}
		if len(body) == 0 || body[len(body)-1] != '\n' {
			fmt.Fprintln(stdout)
		}
		return 0
	}
	return writeHumanServiceJSON(stdout, body)
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
		fmt.Fprintln(writer, compact.String())
		return 0
	}
	fmt.Fprintln(writer, string(trimmed))
	return 0
}

func writeUsageError(writer io.Writer, message string) {
	fmt.Fprintf(writer, "den-tool: %s\nusage: den-tool list [--json] | search <terms...> [--json] | describe <id> [--json] | run <id> -- [extra args...] | board search --project <id> --query <q> [--after-id N] [--limit N] [--json]\n", message)
}

func writeRuntimeError(writer io.Writer, err error) int {
	fmt.Fprintf(writer, "den-tool: %v\n", err)
	return 1
}
