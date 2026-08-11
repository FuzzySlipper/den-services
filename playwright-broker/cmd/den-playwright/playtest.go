package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	broker "den-services/playwright-broker/internal"
)

func runPlaytest(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: den-playwright playtest start|observe|act|inspect|finish|cancel|get|list ...")
	}
	switch args[0] {
	case "start":
		return startPlaytest(args[1:])
	case "observe", "act", "inspect", "finish", "cancel":
		return callPlaytest(args[0], args[1:])
	case "get":
		return getPlaytest(args[1:])
	case "list":
		return listPlaytests(args[1:])
	default:
		return fmt.Errorf("unknown playtest command %q", args[0])
	}
}

func startPlaytest(args []string) error {
	project, rest := splitProjectArg(args)
	var cfgPath, repoRoot, manifestPath, owner, scenario, denProject string
	var denTask int64
	var headed, recordVideo bool
	var viewport string
	var metadataJSON string
	flags := flag.NewFlagSet("den-playwright playtest start", flag.ContinueOnError)
	flags.StringVar(&cfgPath, "config", os.Getenv(configPathEnv), "broker config path")
	flags.StringVar(&repoRoot, "repo", "", "repo root")
	flags.StringVar(&manifestPath, "manifest", "", "manifest path")
	flags.StringVar(&owner, "owner", "", "optional caller correlation label")
	flags.StringVar(&scenario, "scenario", "", "scenario label")
	flags.BoolVar(&headed, "headed", false, "launch headed")
	flags.BoolVar(&recordVideo, "video", false, "record video")
	flags.StringVar(&viewport, "viewport", "", "WIDTHxHEIGHT")
	flags.StringVar(&denProject, "den-project", "", "Den project evidence label")
	flags.Int64Var(&denTask, "den-task", 0, "Den task evidence label")
	flags.StringVar(&metadataJSON, "metadata", "", "arbitrary JSON object retained in evidence")
	if err := flags.Parse(rest); err != nil {
		return err
	}
	cfg, err := loadBrokerConfig(cfgPath)
	if err != nil {
		return err
	}
	metadata := map[string]any{}
	if strings.TrimSpace(metadataJSON) != "" {
		if err := json.Unmarshal([]byte(metadataJSON), &metadata); err != nil {
			metadata["metadata_parse_discrepancy"] = err.Error()
			metadata["metadata_raw"] = metadataJSON
		}
	}
	options := broker.PlaytestStartOptions{
		Project: project, RepoRoot: repoRoot, ManifestPath: manifestPath,
		Owner: owner, Scenario: scenario, DenProjectID: denProject, DenTaskID: denTask, Metadata: metadata,
	}
	if headed {
		options.Headed = &headed
	}
	if recordVideo {
		options.RecordVideo = &recordVideo
	}
	if viewport != "" {
		parsed, parseErr := parseViewport(viewport)
		if parseErr != nil {
			metadata["viewport_discrepancy"] = parseErr.Error()
		} else {
			options.Viewport = &parsed
		}
	}
	session, err := broker.NewPlaytestManager(cfg).Start(context.Background(), options)
	if err != nil {
		return err
	}
	return printJSON(session)
}

func callPlaytest(kind string, args []string) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("session id is required for playtest %s", kind)
	}
	sessionID := args[0]
	var cfgPath, rawRequest string
	flags := flag.NewFlagSet("den-playwright playtest "+kind, flag.ContinueOnError)
	flags.StringVar(&cfgPath, "config", os.Getenv(configPathEnv), "broker config path")
	flags.StringVar(&rawRequest, "request", "{}", "JSON object, @file, or - for stdin")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	cfg, err := loadBrokerConfig(cfgPath)
	if err != nil {
		return err
	}
	raw, err := readRequest(rawRequest)
	if err != nil {
		return err
	}
	request, err := broker.ParseJSONRequest(raw)
	if err != nil {
		request = map[string]any{"request_parse_discrepancy": err.Error(), "raw_request": raw}
	}
	request["kind"] = kind
	result, err := broker.NewPlaytestManager(cfg).Call(context.Background(), sessionID, request)
	if err != nil {
		return err
	}
	return printJSON(result)
}

func getPlaytest(args []string) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return errors.New("session id is required for playtest get")
	}
	var cfgPath string
	flags := flag.NewFlagSet("den-playwright playtest get", flag.ContinueOnError)
	flags.StringVar(&cfgPath, "config", os.Getenv(configPathEnv), "broker config path")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	cfg, err := loadBrokerConfig(cfgPath)
	if err != nil {
		return err
	}
	session, err := broker.NewPlaytestManager(cfg).Get(args[0])
	if err != nil {
		return err
	}
	return printJSON(session)
}

func listPlaytests(args []string) error {
	var cfgPath string
	flags := flag.NewFlagSet("den-playwright playtest list", flag.ContinueOnError)
	flags.StringVar(&cfgPath, "config", os.Getenv(configPathEnv), "broker config path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cfg, err := loadBrokerConfig(cfgPath)
	if err != nil {
		return err
	}
	sessions, err := broker.NewPlaytestManager(cfg).List()
	if err != nil {
		return err
	}
	return printJSON(sessions)
}

func loadBrokerConfig(path string) (*broker.Config, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("-config or %s is required", configPathEnv)
	}
	return broker.LoadConfigFromPath(path)
}

func readRequest(value string) (string, error) {
	if value == "-" {
		data, err := io.ReadAll(os.Stdin)
		return string(data), err
	}
	if strings.HasPrefix(value, "@") {
		data, err := os.ReadFile(strings.TrimPrefix(value, "@"))
		return string(data), err
	}
	return value, nil
}

func parseViewport(value string) (broker.Viewport, error) {
	parts := strings.Split(strings.ToLower(value), "x")
	if len(parts) != 2 {
		return broker.Viewport{}, fmt.Errorf("viewport %q is not WIDTHxHEIGHT", value)
	}
	width, widthErr := strconv.Atoi(parts[0])
	height, heightErr := strconv.Atoi(parts[1])
	if widthErr != nil || heightErr != nil || width <= 0 || height <= 0 {
		return broker.Viewport{}, fmt.Errorf("viewport %q has invalid dimensions", value)
	}
	return broker.Viewport{Width: width, Height: height}, nil
}

func printJSON(value any) error {
	formatted, err := broker.FormatPlaytestResult(value)
	if err != nil {
		return err
	}
	fmt.Print(formatted)
	return nil
}
