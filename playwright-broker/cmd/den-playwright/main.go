package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	broker "den-services/playwright-broker/internal"
)

const configPathEnv = "DEN_PLAYWRIGHT_BROKER_CONFIG_PATH"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: den-playwright run <project> [flags] [-- playwright args...]")
	}
	switch args[0] {
	case "run":
		return runProject(args[1:])
	case "help", "-h", "--help":
		printUsage()
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runProject(args []string) error {
	project, rest := splitProjectArg(args)
	var cfgPath string
	var repoRoot string
	var manifestPath string
	var grep string
	var headed bool
	var playwrightProject string
	var testName string
	var denProjectID string
	var denTaskID int64
	flags := flag.NewFlagSet("den-playwright run", flag.ContinueOnError)
	flags.StringVar(&cfgPath, "config", os.Getenv(configPathEnv), "broker config path")
	flags.StringVar(&repoRoot, "repo", "", "repo root containing the manifest")
	flags.StringVar(&manifestPath, "manifest", "", "manifest path")
	flags.StringVar(&project, "project-id", project, "manifest project id")
	flags.StringVar(&grep, "grep", "", "Playwright grep expression")
	flags.BoolVar(&headed, "headed", false, "run Playwright headed")
	flags.StringVar(&playwrightProject, "pw-project", "", "Playwright project name")
	flags.StringVar(&testName, "test", "", "test file or title argument")
	flags.StringVar(&denProjectID, "den-project", "", "Den project id to include in evidence")
	flags.Int64Var(&denTaskID, "den-task", 0, "Den task id to include in evidence")
	if err := flags.Parse(rest); err != nil {
		return err
	}
	if strings.TrimSpace(cfgPath) == "" {
		return fmt.Errorf("-config or %s is required", configPathEnv)
	}
	cfg, err := broker.LoadConfigFromPath(cfgPath)
	if err != nil {
		return err
	}
	runner := broker.NewRunner(cfg)
	result, err := runner.Run(context.Background(), broker.RunOptions{
		Project:           project,
		RepoRoot:          repoRoot,
		ManifestPath:      manifestPath,
		Grep:              grep,
		Headed:            headed,
		PlaywrightProject: playwrightProject,
		Test:              testName,
		ExtraArgs:         flags.Args(),
		DenProjectID:      denProjectID,
		DenTaskID:         denTaskID,
	})
	if result.Evidence.Artifacts.IndexPath != "" {
		fmt.Printf("evidence=%s\nbase_url=%s\nstatus=%s\n", result.Evidence.Artifacts.IndexPath, result.Evidence.Server.BaseURL, result.Evidence.Status)
	}
	return err
}

func splitProjectArg(args []string) (string, []string) {
	if len(args) == 0 {
		return "", args
	}
	if strings.HasPrefix(args[0], "-") {
		return "", args
	}
	return args[0], args[1:]
}

func printUsage() {
	fmt.Println("usage: den-playwright run <project> [flags] [-- playwright args...]")
	fmt.Println("set " + configPathEnv + " or pass -config")
	fmt.Println("den task ids are copied into the run evidence, not posted automatically")
}
