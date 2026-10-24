// Package provider defines the contract for planning package changes. Provider
// implementations only generate serializable plans; execution lives elsewhere.
package provider

import (
	"errors"

	"github.com/lucasarakaki/phpman/internal/core"
)

// Options controls which components a plan includes.
type Options struct {
	WithFPM         bool
	AllowThirdParty bool
}

// DefaultOptions returns the MVP defaults: CLI and FPM.
func DefaultOptions() Options {
	return Options{WithFPM: true}
}

// Provider generates plans for one package-manager family.
type Provider interface {
	PlanInstall(snap *core.Snapshot, raw string, opts Options) (*core.Plan, error)
	PlanRemove(snap *core.Snapshot, raw string) (*core.Plan, error)
	PlanSwitch(snap *core.Snapshot, raw string) (*core.Plan, error)
}

// Plan errors. They are sentinels so the CLI can distinguish invalid requests
// from unexpected failures and choose the right exit code.
var (
	ErrUnsupportedDistro     = errors.New("distribuição sem suporte a planos de escrita")
	ErrInvalidVersion        = errors.New("versão inválida; use o formato X.Y")
	ErrVersionNotInstalled   = errors.New("versão de PHP não instalada")
	ErrVersionUnavailable    = errors.New("versão indisponível no repositório configurado")
	ErrActiveVersion         = errors.New("não é possível remover a versão ativa")
	ErrNoPackages            = errors.New("não foi possível identificar os pacotes da versão")
	ErrThirdPartyUnsupported = errors.New("repositório de terceiros indisponível para esta distribuição")
)
