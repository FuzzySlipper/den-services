package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const buildUsage = "usage: den-tool build run --repo REPO --lane LANE --ref COMMIT [--host HOST] [--no-wait] [--config FILE] [--url URL]"

const (
	buildWorkSource = "build"
	buildLockBusy   = "den-build: another routed build holds the worker lock"
	buildLockExit   = 75
)

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
	noWait := flags.Bool("no-wait", false, "return immediately if the workers are busy")
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runBuild(ctx, job, config, *host, config.WaitForWorker && !*noWait, client, stdout, stderr)
}

func runBuild(ctx context.Context, job buildJob, config buildConfig, onlyHost string, wait bool, client *ResourcesClient, stdout, stderr io.Writer) int {
	var checkedBusy map[string]bool
	if wait {
		checkedBusy = make(map[string]bool)
	}
	queued := false
	for ctx.Err() == nil {
		worker, busy, err := selectBuildHost(ctx, client, config, job, onlyHost, checkedBusy)
		if ctx.Err() != nil {
			break
		}
		if err == nil {
			code, lockBusy := runBuildOnHost(ctx, job, worker, client, stdout, stderr)
			if !lockBusy {
				return code
			}
			busy = true
			err = fmt.Errorf("no eligible host for %s/%s (%s: another routed build holds the worker lock)", job.repo, job.lane, worker.ID)
		}
		if !wait || !busy {
			return writeRuntimeError(stderr, err)
		}
		if !queued {
			_, _ = fmt.Fprintf(stderr, "den-tool build: queued %s/%s at %s. Waiting for a compatible worker.\n", job.repo, job.lane, job.ref)
			queued = true
		}
		select {
		case <-ctx.Done():
		case <-time.After(config.queuePollInterval()):
		}
	}
	_, _ = fmt.Fprintln(stderr, "den-tool build: cancelled before starting the lane")
	return 130
}

func runBuildOnHost(ctx context.Context, job buildJob, worker buildHost, client *ResourcesClient, stdout, stderr io.Writer) (int, bool) {
	input, connection, err := os.Pipe()
	if err != nil {
		return writeRuntimeError(stderr, err), false
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
	child := exec.CommandContext(ctx, argv[0], argv[1:]...)
	// runForwardingSignals handles cancellation after SSH has started.
	child.Cancel = nil
	tail := &tailBuffer{limit: stderrTailBytes}
	child.Stdin, child.Stdout, child.Stderr = input, stdout, io.MultiWriter(stderr, tail)
	lockBusy := func(code int) bool {
		return code == buildLockExit && strings.HasSuffix(tail.String(), buildLockBusy+" "+runID)
	}

	_, _ = fmt.Fprintf(stderr, "den-tool build: %s/%s at %s on %s (%d jobs)\n", job.repo, job.lane, job.ref, worker.ID, worker.Jobs)
	report := resourceWorkReport{
		Host: worker.ID, Title: job.repo + " / " + job.lane, Owner: defaultOwner(), Project: job.repo,
		Resources: []string{}, State: "running", TTLSeconds: defaultWorkTTLSeconds, Source: buildWorkSource,
		ExternalID: runID, Note: "Commit " + job.ref,
	}
	code := runReportedCommand(ctx, child, client, report, stderr, lockBusy)
	return code, lockBusy(code)
}
