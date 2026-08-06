# Runtime dependency and licence inventory

Generated from the production package graph on 2026-08-06 with Go 1.26.5.
`charm.land/bubbletea/v2` v2.0.8 is the sole direct Go dependency. All other Go
modules below are reached transitively through Bubble Tea's terminal runtime.
There is no monitor-control library, DLL, or executable dependency.

| Module | Version | Licence file classification |
| --- | --- | --- |
| `charm.land/bubbletea/v2` | v2.0.8 | MIT |
| `github.com/charmbracelet/colorprofile` | v0.4.3 | MIT |
| `github.com/charmbracelet/ultraviolet` | f5a850f9c2b7 (2026-07-03) | MIT |
| `github.com/charmbracelet/x/ansi` | v0.11.7 | MIT |
| `github.com/charmbracelet/x/term` | v0.2.2 | MIT |
| `github.com/charmbracelet/x/termios` | v0.1.1 | MIT |
| `github.com/charmbracelet/x/windows` | v0.2.2 | MIT |
| `github.com/clipperhouse/displaywidth` | v0.11.0 | MIT |
| `github.com/clipperhouse/uax29/v2` | v2.7.0 | MIT |
| `github.com/lucasb-eyer/go-colorful` | v1.4.0 | MIT |
| `github.com/mattn/go-runewidth` | v0.0.23 | MIT |
| `github.com/muesli/cancelreader` | v0.2.2 | MIT |
| `github.com/rivo/uniseg` | v0.4.7 | MIT |
| `github.com/xo/terminfo` | abceb7e1c41e | MIT |
| `golang.org/x/sync` | v0.21.0 | Go project BSD-style licence |
| `golang.org/x/sys` | v0.46.0 | Go project BSD-style licence |

The validation gate runs the pinned official `govulncheck` v1.6.0 against the
reachable call graph. The 2026-08-06 run reported no known vulnerabilities.
Release packaging must include the applicable licence texts and regenerate this
inventory from the exact release commit.
