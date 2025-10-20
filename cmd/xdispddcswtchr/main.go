package main

import (
	"fmt"
	"log"
	"runtime"

	"github.com/markz0r/XDispDDCSwtchr/internal/config"
	"github.com/markz0r/XDispDDCSwtchr/internal/ddc"
	"github.com/markz0r/XDispDDCSwtchr/internal/hotkeys"
	"github.com/markz0r/XDispDDCSwtchr/internal/logic"
)

func main() {
	cfg, err := config.Load("XDispDDCSwtchr-Settings.json")
	if err != nil {
		log.Fatal(err)
	}

	backend, err := selectBackend(cfg)
	if err != nil {
		log.Fatal(err)
	}

	engine := logic.NewEngine(cfg, backend)
	binds := map[string]hotkeys.Handler{}
	for _, hk := range cfg.Hotkeys {
		h := hk
		binds[h.Keys] = func() {
			if err := engine.Dispatch(h.Action, h.Args, h.Target); err != nil {
				log.Printf("%s failed: %v", h.Action, err)
			} else {
				log.Printf("%s OK", h.Action)
			}
		}
	}

	stop := hotkeys.Register(binds)
	defer stop()
	log.Printf("XDispDDCSwtchr started on %s (backend=%s)", runtime.GOOS, cfg.Backend)
	select {}
}

func selectBackend(cfg *config.Settings) (ddc.Backend, error) {
	backend := cfg.Backend
	if backend == "" {
		backend = config.BackendCLI
	}
	cfg.Backend = backend

	switch backend {
	case config.BackendCLI:
		return makeCLI(cfg), nil
	case config.BackendNative:
		b, err := ddc.MakeNative(cfg)
		if err != nil {
			return nil, fmt.Errorf("native backend unavailable: %w", err)
		}
		return b, nil
	default:
		return nil, fmt.Errorf("unsupported backend: %s", backend)
	}
}

func makeCLI(cfg *config.Settings) ddc.Backend {
	switch runtime.GOOS {
	case "windows":
		return &ddc.CLIBackend{WinControlMyMonPath: cfg.CLI.WinControlMyMonPath}
	case "darwin", "linux":
		return &ddc.CLIBackend{
			LinuxDdcutilPath: cfg.CLI.LinuxDdcutilPath,
			MacDdcctlPath:    cfg.CLI.MacDdcctlPath,
		}
	default:
		log.Fatalf("unsupported OS %s for CLI backend", runtime.GOOS)
		return nil
	}
}
