package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

const buildUsage = "usage: den-tool build run --repo REPO --lane LANE --ref COMMIT [--host HOST] [--config FILE] [--url URL]"

const buildWorkSource = "build"

type buildJob struct {
	repo, lane, ref string
	repository      buildRepository
	settings        buildLane
}

func handleBuild(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || slices.Contains([]string{"help", "-h", "--help"}, args[0]) {
		_, _ = fmt.Fprintln(stderr, buildUsage)
		return 0
	}
	if args[0] != "run" {
		_, _ = fmt.Fprintf(stderr, "den-tool build: unknown command %q\n%s\n", args[0], buildUsage)
		return 2
	}
	flags := flag.NewFlagSet("build run", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() { _, _ = fmt.Fprintln(stderr, buildUsage) }
	repo := flags.String("repo", "", "configured repository")
	lane := flags.String("lane", "", "configured headless lane")
	ref := flags.String("ref", "", "full Git commit SHA")
	host := flags.String("host", "", "only consider this build host")
	configPath := flags.String("config", os.Getenv("DEN_BUILD_CONFIG_PATH"), "build config file")
	url := flags.String("url", "", "Resources service base URL")
	if err := flags.Parse(args[1:]); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	decoded, err := hex.DecodeString(*ref)
	if flags.NArg() != 0 || *repo == "" || *lane == "" || err != nil || len(decoded) != 20 {
		_, _ = fmt.Fprintln(stderr, "den-tool build: provide a repo, a headless lane, and a full Git commit SHA")
		flags.Usage()
		return 2
	}
	if *configPath == "" {
		configDir, err := os.UserConfigDir()
		if err != nil {
			return writeRuntimeError(stderr, err)
		}
		*configPath = filepath.Join(configDir, "den-build", "config.json")
	}
	config, err := loadBuildConfig(*configPath)
	if err != nil {
		return writeRuntimeError(stderr, err)
	}
	repository := config.Repositories[*repo]
	settings, ok := repository.Lanes[*lane]
	if !ok {
		_, _ = fmt.Fprintf(stderr, "den-tool build: %s/%s is outside the configured headless lanes. Use the usual host for GPU, window and performance lanes.\n", *repo, *lane)
		return 2
	}
	if *host != "" && !slices.ContainsFunc(config.Hosts, func(worker buildHost) bool { return worker.ID == *host && worker.Enabled }) {
		return writeRuntimeError(stderr, fmt.Errorf("build host %q is not enabled in %s", *host, *configPath))
	}
	client, err := NewResourcesClient(resourcesURL(*url))
	if err != nil {
		return writeRuntimeError(stderr, err)
	}
	job := buildJob{repo: *repo, lane: *lane, ref: strings.ToLower(*ref), repository: repository, settings: settings}
	return runBuild(context.Background(), job, config, *host, client, stdout, stderr)
}

func runBuild(ctx context.Context, job buildJob, config buildConfig, onlyHost string, client *ResourcesClient, stdout, stderr io.Writer) int {
	worker, err := selectBuildHost(ctx, client, config, job, onlyHost)
	if err != nil {
		return writeRuntimeError(stderr, err)
	}
	input, connection, err := os.Pipe()
	if err != nil {
		return writeRuntimeError(stderr, err)
	}
	defer input.Close()
	defer connection.Close()
	// Keep SSH stdin open so the worker can stop the build if SSH disconnects.
	// The lane itself has no stdin.
	runID := randomID()
	arguments := []string{job.repository.Origin, runID, strconv.Itoa(len(job.repository.Keep))}
	arguments = append(arguments, job.repository.Keep...)
	arguments = append(arguments, job.settings.Command...)
	argv := job.remoteArgv(worker, "run", arguments)
	child := exec.Command(argv[0], argv[1:]...)
	child.Stdin, child.Stdout, child.Stderr = input, stdout, stderr

	_, _ = fmt.Fprintf(stderr, "den-tool build: %s/%s at %s on %s (%d jobs)\n", job.repo, job.lane, job.ref, worker.ID, worker.Jobs)
	report := resourceWorkReport{
		Host: worker.ID, Title: job.repo + " / " + job.lane, Owner: defaultOwner(), Project: job.repo,
		Resources: []string{}, State: "running", TTLSeconds: defaultWorkTTLSeconds, Source: buildWorkSource,
		ExternalID: runID, Note: "Commit " + job.ref,
	}
	return runReportedCommand(ctx, child, client, report, stderr)
}
