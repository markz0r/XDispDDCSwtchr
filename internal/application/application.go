package application

import (
	"github.com/markz0r/XDispDDCSwtchr/internal/backend"
	"github.com/markz0r/XDispDDCSwtchr/internal/platform"
	"github.com/markz0r/XDispDDCSwtchr/internal/profiles"
	"github.com/markz0r/XDispDDCSwtchr/internal/service"
)

type Runtime struct {
	Service  *service.Service
	Platform platform.Info
}

func New(applicationCommit string) (*Runtime, error) {
	native, err := backend.New()
	if err != nil {
		return nil, err
	}
	registry, err := profiles.NewRegistry()
	if err != nil {
		return nil, err
	}
	matrix, err := profiles.LoadSupportMatrix()
	if err != nil {
		return nil, err
	}
	current, err := platform.Current()
	if err != nil {
		return nil, err
	}
	appService := service.New(native, registry, matrix, profiles.Platform{
		OS:                current.OS,
		Architecture:      current.Architecture,
		Version:           current.Version,
		Build:             current.Build,
		ApplicationCommit: applicationCommit,
	})
	return &Runtime{Service: appService, Platform: current}, nil
}
