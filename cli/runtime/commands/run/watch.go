package run

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/wago-org/wago/cli/internal/ui"
)

func watchModule(path, intervalValue string) {
	interval := 200 * time.Millisecond
	if intervalValue != "" {
		parsed, err := time.ParseDuration(intervalValue)
		if err != nil || parsed <= 0 {
			ui.Fatal("run: --watch-interval must be a positive duration")
		}
		interval = parsed
	}
	arguments := withoutWatchFlags(os.Args[1:])
	runWatched(arguments)
	stamp, err := fileStamp(path)
	if err != nil {
		ui.Fatal("watch: %v", err)
	}
	fmt.Fprintf(os.Stderr, "%s watching %s\n", ui.Dim("→"), path)
	for range time.NewTicker(interval).C {
		next, err := fileStamp(path)
		if err != nil {
			continue
		}
		if next == stamp {
			continue
		}
		stamp = next
		fmt.Fprintf(os.Stderr, "%s changed %s\n", ui.Dim("→"), path)
		runWatched(arguments)
	}
}

type watchedStamp struct {
	modTime int64
	size    int64
}

func fileStamp(path string) (watchedStamp, error) {
	info, err := os.Stat(path)
	if err != nil {
		return watchedStamp{}, err
	}
	return watchedStamp{modTime: info.ModTime().UnixNano(), size: info.Size()}, nil
}

func runWatched(arguments []string) {
	command := exec.Command(os.Args[0], arguments...)
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := command.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "%s %v\n", ui.Red("wago:"), err)
	}
}

func withoutWatchFlags(arguments []string) []string {
	result := make([]string, 0, len(arguments))
	passThrough := false
	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]
		if passThrough {
			result = append(result, argument)
			continue
		}
		if argument == "--" {
			passThrough = true
			result = append(result, argument)
			continue
		}
		if argument == "--watch" || argument == "-w" {
			continue
		}
		if argument == "--watch-interval" {
			if index+1 < len(arguments) {
				index++
			}
			continue
		}
		if len(argument) > len("--watch-interval=") && argument[:len("--watch-interval=")] == "--watch-interval=" {
			continue
		}
		result = append(result, argument)
		if LooksLikeTarget(argument) {
			passThrough = true
		}
	}
	return result
}
