package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/markz0r/XDispDDCSwtchr/internal/application"
	"github.com/markz0r/XDispDDCSwtchr/internal/cli"
	"github.com/markz0r/XDispDDCSwtchr/internal/config"
	"github.com/markz0r/XDispDDCSwtchr/internal/tui"
)

var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	os.Exit(run(context.Background(), os.Args[1:]))
}

func run(ctx context.Context, args []string) int {
	configPath, hasCommand, parseErr := invocation(args)
	if parseErr != nil {
		fmt.Fprintln(os.Stderr, parseErr)
		return 2
	}
	if !hasCommand {
		if !terminal(os.Stdin) || !terminal(os.Stdout) {
			return newCLIRunner().Run(ctx, nil)
		}
		path, err := config.ResolvePath(configPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		settings, err := config.Load(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return cli.ExitCode(err)
		}
		runtime, err := application.New(commit)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return cli.ExitCode(err)
		}
		store := config.NewQualificationStore(path)
		if err := runtime.Service.ConfigureUserQualifications(settings.UserQualifications, store.Save); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return cli.ExitCode(err)
		}
		program := tea.NewProgram(tui.New(runtime.Service, settings.PreferredMonitorID))
		if _, err := program.Run(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 5
		}
		return 0
	}
	return newCLIRunner().Run(ctx, args)
}

func newCLIRunner() *cli.Runner {
	return cli.New(cli.Dependencies{
		Version: version,
		Commit:  commit,
		NewRuntime: func() (cli.Runtime, error) {
			runtime, err := application.New(commit)
			if err != nil {
				return cli.Runtime{}, err
			}
			return cli.Runtime{Service: runtime.Service, Platform: runtime.Platform}, nil
		},
	}, os.Stdout, os.Stderr)
}

func invocation(args []string) (configPath string, hasCommand bool, err error) {
	flags := flag.NewFlagSet("xdispddcswtchr", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.StringVar(&configPath, "config", "", "configuration file")
	if err := flags.Parse(args); err != nil {
		return "", false, err
	}
	return configPath, flags.NArg() > 0, nil
}

func terminal(file *os.File) bool {
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
