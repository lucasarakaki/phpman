// Package registry selects a plan provider for the detected distribution.
package registry

import (
	"fmt"

	"github.com/lucasarakaki/phpman/internal/core"
	"github.com/lucasarakaki/phpman/internal/provider"
	"github.com/lucasarakaki/phpman/internal/provider/apt"
)

// For returns the provider for a distribution family.
func For(d core.Distro) (provider.Provider, error) {
	switch d.Family {
	case core.FamilyDebian:
		return apt.New(), nil
	default:
		return nil, fmt.Errorf("%w: %s", provider.ErrUnsupportedDistro, d.Family)
	}
}
