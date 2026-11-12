package main

import (
	_ "embed"
	"maps"
	"slices"
	"strconv"
	"strings"
)

//go:embed build_worker.sh
var buildWorkerScript string

func (job buildJob) remoteArgv(host buildHost, mode string, extra []string) []string {
	command := []string{"env"}
	for _, key := range slices.Sorted(maps.Keys(job.settings.Env)) {
		command = append(command, key+"="+job.settings.Env[key])
	}
	command = append(command, "bash", "-c", buildWorkerScript, "den-build", mode,
		host.BuildRoot, job.repo, strings.TrimRight(host.SourceRoot, "/")+"/"+job.repo,
		job.ref, strconv.Itoa(host.Jobs), strconv.Itoa(job.settings.MinFreeGiB), job.settings.OS)
	command = append(command, extra...)
	for i, word := range command {
		command[i] = shellQuote(word)
	}
	return append(sshArgv(append([]string{"-T"}, host.SSH...)), strings.Join(command, " "))
}
