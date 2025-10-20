package logic

import (
	"fmt"
	"sort"
	"strings"

	"github.com/markz0r/XDispDDCSwtchr/internal/config"
	"github.com/markz0r/XDispDDCSwtchr/internal/ddc"
)

type Engine struct {
	cfg       *config.Settings
	backend   ddc.Backend
	iterState map[string]map[string]int
}

func NewEngine(cfg *config.Settings, backend ddc.Backend) *Engine {
	return &Engine{cfg: cfg, backend: backend, iterState: map[string]map[string]int{}}
}

func (e *Engine) monitorModel(monitorID string) (*config.Model, error) {
	var modelName string
	if monitorID != "" {
		for _, m := range e.cfg.Monitors {
			if m.Identifier == monitorID {
				modelName = m.Model
				break
			}
		}
	} else if len(e.cfg.Monitors) > 0 {
		modelName = e.cfg.Monitors[0].Model
	}
	if modelName == "" {
		return nil, fmt.Errorf("no monitor/model match for '%s'", monitorID)
	}
	for i := range e.cfg.Models {
		if e.cfg.Models[i].Name == modelName {
			return &e.cfg.Models[i], nil
		}
	}
	return nil, fmt.Errorf("model not found: %s", modelName)
}

func (e *Engine) setInputSource(monitorID, sourceName string) error {
	md, err := e.monitorModel(monitorID)
	if err != nil {
		return err
	}
	val, ok := md.InputSelect.Values[sourceName]
	if !ok {
		return fmt.Errorf("unknown input '%s' for model %s", sourceName, md.Name)
	}
	return e.backend.SetVCP(monitorID, md.InputSelect.Code, val)
}

func (e *Engine) nextInputSource(monitorID string) error {
	md, err := e.monitorModel(monitorID)
	if err != nil {
		return err
	}
	keys := make([]string, 0, len(md.InputSelect.Values))
	for k := range md.InputSelect.Values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return fmt.Errorf("no inputs configured")
	}
	i := e.bumpIndex(monitorID, "input", len(keys))
	return e.setInputSource(monitorID, keys[i])
}

func (e *Engine) togglePIP(monitorID string) error {
	md, err := e.monitorModel(monitorID)
	if err != nil {
		return err
	}
	if md.PIP == nil {
		return fmt.Errorf("PIP not configured for model %s", md.Name)
	}
	on := e.bumpIndex(monitorID, "pip_on", 2) == 1
	val := md.PIP.OffValue
	if on {
		val = md.PIP.OnValue
	}
	return e.backend.SetVCP(monitorID, md.PIP.ToggleCode, val)
}

func (e *Engine) nextPIPSource(monitorID string) error {
	md, err := e.monitorModel(monitorID)
	if err != nil {
		return err
	}
	if md.PIP == nil || md.PIP.SourceCode == "" || len(md.PIP.Sources) == 0 {
		return fmt.Errorf("PIP sources not configured")
	}
	idx := e.bumpIndex(monitorID, "pip_src", len(md.PIP.Sources))
	srcName := md.PIP.Sources[idx]
	val, ok := e.lookupNamed(md, "PIP_Source_"+srcName)
	if !ok {
		return fmt.Errorf("no value for PIP_Source_%s", srcName)
	}
	return e.backend.SetVCP(monitorID, md.PIP.SourceCode, val)
}

func (e *Engine) nextPIPPosition(monitorID string) error {
	md, err := e.monitorModel(monitorID)
	if err != nil {
		return err
	}
	if md.PIP == nil || md.PIP.PositionCode == "" || len(md.PIP.Positions) == 0 {
		return fmt.Errorf("PIP positions not configured")
	}
	idx := e.bumpIndex(monitorID, "pip_pos", len(md.PIP.Positions))
	name := md.PIP.Positions[idx]
	val, ok := e.lookupNamed(md, "PIP_Pos_"+name)
	if !ok {
		return fmt.Errorf("no value for PIP_Pos_%s", name)
	}
	return e.backend.SetVCP(monitorID, md.PIP.PositionCode, val)
}

func (e *Engine) nextPIPSize(monitorID string) error {
	md, err := e.monitorModel(monitorID)
	if err != nil {
		return err
	}
	if md.PIP == nil || md.PIP.SizeCode == "" || len(md.PIP.Sizes) == 0 {
		return fmt.Errorf("PIP sizes not configured")
	}
	idx := e.bumpIndex(monitorID, "pip_size", len(md.PIP.Sizes))
	name := md.PIP.Sizes[idx]
	val, ok := e.lookupNamed(md, "PIP_Size_"+name)
	if !ok {
		return fmt.Errorf("no value for PIP_Size_%s", name)
	}
	return e.backend.SetVCP(monitorID, md.PIP.SizeCode, val)
}

func (e *Engine) swapPIP(monitorID string) error {
	md, err := e.monitorModel(monitorID)
	if err != nil {
		return err
	}
	if md.PIP == nil || md.PIP.SwapCode == "" {
		return fmt.Errorf("PIP swap not configured")
	}
	return e.backend.SetVCP(monitorID, md.PIP.SwapCode, md.PIP.SwapValue)
}

func (e *Engine) bumpIndex(monitorID, feature string, n int) int {
	if e.iterState[monitorID] == nil {
		e.iterState[monitorID] = map[string]int{}
	}
	i := e.iterState[monitorID][feature]
	i = (i + 1) % n
	e.iterState[monitorID][feature] = i
	return i
}

func (e *Engine) lookupNamed(md *config.Model, key string) (uint16, bool) {
	if md.VCPNamedValues == nil {
		return 0, false
	}
	v, ok := md.VCPNamedValues[key]
	return v, ok
}

func (e *Engine) Dispatch(action string, args map[string]string, target string) error {
	switch strings.ToLower(action) {
	case "setinputsource":
		src := args["source"]
		if src == "" {
			return fmt.Errorf("missing args.source")
		}
		return e.setInputSource(target, src)
	case "nextinputsource":
		return e.nextInputSource(target)
	case "togglepip":
		return e.togglePIP(target)
	case "nextpipsource":
		return e.nextPIPSource(target)
	case "nextpipposition":
		return e.nextPIPPosition(target)
	case "nextpipsize":
		return e.nextPIPSize(target)
	case "swappip":
		return e.swapPIP(target)
	default:
		return fmt.Errorf("unknown action: %s", action)
	}
}
