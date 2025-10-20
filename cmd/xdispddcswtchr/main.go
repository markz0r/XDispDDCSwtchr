package main

import (
	"log"
	"runtime"

	"github.com/markz0r/XDispDDCSwtchr/internal/config"
	"github.com/markz0r/XDispDDCSwtchr/internal/ddc"
	"github.com/markz0r/XDispDDCSwtchr/internal/hotkeys"
	"github.com/markz0r/XDispDDCSwtchr/internal/logic"
)

func main() {
	cfg, err := config.Load("XDispDDCSwtchr-Settings.json")
	if err != nil { log.Fatal(err) }

	var backend ddc.Backend
	switch cfg.Backend {
	case config.BackendCLI:
		backend = makeCLI(cfg)
	case config.BackendNative:
		b, err := ddc.MakeNative(cfg)
		if err != nil { log.Fatalf("native backend unavailable: %v", err) }
		cmd/xdispddcswtchr/main.gobackend = b
	default:
		log.Fatalf("unsupported backend: %s", cfg.Backend)
	}

	engine := logic.NewEngine(cfg, backend)
	binds := map[string]func(){}
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
		log.Fatal("unsupported OS for CLI backend")
		return nil
	}
}

