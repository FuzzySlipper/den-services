package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	serve "den-services/den-serve/internal"
	devserver "den-services/devserver-broker"
)

const configPathEnv = "DEN_SERVE_CONFIG_PATH"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		printUsage()
		return errors.New("missing command")
	}
	switch args[0] {
	case "up":
		return runUp(args[1:])
	case "restart":
		return runRestart(args[1:])
	case "status":
		return runStatus(args[1:])
	case "list":
		return runList(args[1:])
	case "stop":
		return runStop(args[1:])
	case "logs":
		return runLogs(args[1:])
	case "help", "-h", "--help":
		printUsage()
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

type restartManager interface {
	Status(context.Context, devserver.StatusOptions) (devserver.SessionState, error)
	Stop(context.Context, devserver.StopOptions) (devserver.StopResult, error)
	Up(context.Context, devserver.UpOptions) (devserver.UpResult, error)
}

func runUp(args []string) error {
	project, rest := splitProjectArg(args)
	var cfgPath string
	var repoRoot string
	var manifestPath string
	var publicHost string
	flags := flag.NewFlagSet("den-serve up", flag.ContinueOnError)
	flags.StringVar(&cfgPath, "config", os.Getenv(configPathEnv), "config path")
	flags.StringVar(&repoRoot, "repo", "", "repo root containing the manifest")
	flags.StringVar(&manifestPath, "manifest", "", "manifest path")
	flags.StringVar(&project, "project-id", project, "manifest project id")
	flags.StringVar(&publicHost, "public-host", "", "LAN host/IP to print when auto-detection is wrong")
	if err := flags.Parse(rest); err != nil {
		return err
	}
	manager, err := newManager(cfgPath)
	if err != nil {
		return err
	}
	result, err := manager.Up(context.Background(), devserver.UpOptions{
		Project:            project,
		RepoRoot:           repoRoot,
		ManifestPath:       manifestPath,
		PublicHostOverride: publicHost,
	})
	if result.Session.Project != "" {
		printSessionPacket(result.Session)
	}
	return err
}

func runRestart(args []string) error {
	project, rest := splitProjectArg(args)
	var cfgPath string
	var repoRoot string
	var manifestPath string
	var publicHost string
	flags := flag.NewFlagSet("den-serve restart", flag.ContinueOnError)
	flags.StringVar(&cfgPath, "config", os.Getenv(configPathEnv), "config path")
	flags.StringVar(&repoRoot, "repo", "", "repo root containing the manifest")
	flags.StringVar(&manifestPath, "manifest", "", "manifest path")
	flags.StringVar(&project, "project-id", project, "manifest project id")
	flags.StringVar(&publicHost, "public-host", "", "LAN host/IP to print when auto-detection is wrong")
	if err := flags.Parse(rest); err != nil {
		return err
	}
	manager, err := newManager(cfgPath)
	if err != nil {
		return err
	}
	result, err := restart(context.Background(), manager, devserver.UpOptions{
		Project:            project,
		RepoRoot:           repoRoot,
		ManifestPath:       manifestPath,
		PublicHostOverride: publicHost,
	})
	if result.Session.Project != "" {
		printSessionPacket(result.Session)
	}
	return err
}

func restart(ctx context.Context, manager restartManager, options devserver.UpOptions) (devserver.UpResult, error) {
	session, err := manager.Status(ctx, devserver.StatusOptions{Project: options.Project, RepoRoot: options.RepoRoot})
	switch {
	case errors.Is(err, devserver.ErrSessionNotFound):
		// Nothing is up yet, so restart has the same result as up.
	case err != nil:
		return devserver.UpResult{}, err
	case session.Ownership != "broker_owned" && session.Status != "stopped":
		return devserver.UpResult{}, errors.New("session is not broker-owned; refusing to restart an external process")
	case session.Status != "stopped":
		if _, err := manager.Stop(ctx, devserver.StopOptions{Project: options.Project, RepoRoot: options.RepoRoot}); err != nil {
			return devserver.UpResult{}, err
		}
	}
	return manager.Up(ctx, options)
}

func runStatus(args []string) error {
	project, rest := splitProjectArg(args)
	var cfgPath string
	var repoRoot string
	flags := flag.NewFlagSet("den-serve status", flag.ContinueOnError)
	flags.StringVar(&cfgPath, "config", os.Getenv(configPathEnv), "config path")
	flags.StringVar(&repoRoot, "repo", "", "repo root for disambiguating sessions")
	flags.StringVar(&project, "project-id", project, "manifest project id")
	if err := flags.Parse(rest); err != nil {
		return err
	}
	manager, err := newManager(cfgPath)
	if err != nil {
		return err
	}
	session, err := manager.Status(context.Background(), devserver.StatusOptions{Project: project, RepoRoot: repoRoot})
	if err != nil {
		return err
	}
	printSessionPacket(session)
	return nil
}

func runList(args []string) error {
	var cfgPath string
	flags := flag.NewFlagSet("den-serve list", flag.ContinueOnError)
	flags.StringVar(&cfgPath, "config", os.Getenv(configPathEnv), "config path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	manager, err := newManager(cfgPath)
	if err != nil {
		return err
	}
	sessions, err := manager.List(context.Background())
	if err != nil {
		return err
	}
	for _, session := range sessions {
		url := session.LANURL
		if url == "" {
			url = session.LocalURL
		}
		fmt.Printf("%s\t%s\t%s\t%s\t%s\n", session.Project, session.Status, url, session.RepoRoot, session.StatePath)
	}
	return nil
}

func runStop(args []string) error {
	project, rest := splitProjectArg(args)
	var cfgPath string
	var repoRoot string
	flags := flag.NewFlagSet("den-serve stop", flag.ContinueOnError)
	flags.StringVar(&cfgPath, "config", os.Getenv(configPathEnv), "config path")
	flags.StringVar(&repoRoot, "repo", "", "repo root for disambiguating sessions")
	flags.StringVar(&project, "project-id", project, "manifest project id")
	if err := flags.Parse(rest); err != nil {
		return err
	}
	manager, err := newManager(cfgPath)
	if err != nil {
		return err
	}
	result, err := manager.Stop(context.Background(), devserver.StopOptions{Project: project, RepoRoot: repoRoot})
	if err != nil {
		return err
	}
	fmt.Println(result.Message)
	printSessionPacket(result.Session)
	return nil
}

func runLogs(args []string) error {
	project, rest := splitProjectArg(args)
	var cfgPath string
	var repoRoot string
	flags := flag.NewFlagSet("den-serve logs", flag.ContinueOnError)
	flags.StringVar(&cfgPath, "config", os.Getenv(configPathEnv), "config path")
	flags.StringVar(&repoRoot, "repo", "", "repo root for disambiguating sessions")
	flags.StringVar(&project, "project-id", project, "manifest project id")
	if err := flags.Parse(rest); err != nil {
		return err
	}
	manager, err := newManager(cfgPath)
	if err != nil {
		return err
	}
	session, err := manager.Status(context.Background(), devserver.StatusOptions{Project: project, RepoRoot: repoRoot})
	if err != nil {
		return err
	}
	fmt.Printf("stdout: %s\n", session.StdoutLog)
	printTail(session.StdoutLog)
	fmt.Printf("stderr: %s\n", session.StderrLog)
	printTail(session.StderrLog)
	return nil
}

func newManager(cfgPath string) (*devserver.Manager, error) {
	var cfg devserver.ManagerConfig
	var err error
	if strings.TrimSpace(cfgPath) == "" {
		cfg, err = serve.DefaultConfig()
	} else {
		cfg, err = serve.LoadConfigFromPath(cfgPath)
	}
	if err != nil {
		return nil, err
	}
	return devserver.NewManager(cfg)
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

func printSessionPacket(session devserver.SessionState) {
	fmt.Print(formatSessionPacket(session))
}

func formatSessionPacket(session devserver.SessionState) string {
	var packet strings.Builder
	fmt.Fprintf(&packet, "%s %s\n", session.Project, session.Status)
	fmt.Fprintf(&packet, "local: %s\n", session.LocalURL)
	if session.LANURL != "" {
		fmt.Fprintf(&packet, "lan:   %s\n", session.LANURL)
	} else {
		fmt.Fprintln(&packet, "lan:   unavailable")
	}
	fmt.Fprintf(&packet, "state: %s\n", session.StatePath)
	fmt.Fprintf(&packet, "logs:  %s\n", session.SessionDir)
	fmt.Fprintf(&packet, "launch source: %s\n", session.ReuseSource)
	fmt.Fprintf(&packet, "started: %s\n", session.StartedAt.UTC().Format("2006-01-02T15:04:05.999999999Z"))
	if session.LaunchFingerprint.Value != "" {
		fmt.Fprintf(&packet, "launch fingerprint:  %s\n", session.LaunchFingerprint.Value)
		fmt.Fprintf(&packet, "current fingerprint: %s\n", session.CurrentFingerprint.Value)
		if session.LaunchFingerprint.RepoHead != "" {
			fmt.Fprintf(&packet, "launch repo HEAD:    %s\n", session.LaunchFingerprint.RepoHead)
		}
	}
	if session.Stale {
		fmt.Fprintf(&packet, "stale: true (%s)\n", session.StaleReason)
	}
	if session.FingerprintError != "" {
		fmt.Fprintf(&packet, "fingerprint error: %s\n", session.FingerprintError)
	}
	if session.PID > 0 {
		fmt.Fprintf(&packet, "pid:   %d\n", session.PID)
	}
	return packet.String()
}

func printTail(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Println("(log file does not exist yet)")
			return
		}
		fmt.Printf("(could not read log: %v)\n", err)
		return
	}
	const limit = 12 * 1024
	if len(data) > limit {
		data = data[len(data)-limit:]
		fmt.Println("(last 12 KiB)")
	}
	if len(data) == 0 {
		fmt.Println("(empty)")
		return
	}
	fmt.Println(strings.TrimRight(string(data), "\n"))
}

func printUsage() {
	name := filepath.Base(os.Args[0])
	fmt.Printf("usage: %s up <project> -repo /path/to/repo [--public-host ip]\n", name)
	fmt.Printf("       %s restart <project> -repo /path/to/repo [--public-host ip]\n", name)
	fmt.Printf("       %s status <project> [-repo /path/to/repo]\n", name)
	fmt.Printf("       %s list\n", name)
	fmt.Printf("       %s stop <project> [-repo /path/to/repo]\n", name)
	fmt.Printf("       %s logs <project> [-repo /path/to/repo]\n", name)
	fmt.Println("pass -config or set " + configPathEnv + " only to override built-in defaults")
}
