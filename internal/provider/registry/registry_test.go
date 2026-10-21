package registry

import (
	"errors"
	"testing"

	"github.com/lucasarakaki/phpman/internal/core"
	"github.com/lucasarakaki/phpman/internal/provider"
)

func TestForDebian(t *testing.T) {
	prov, err := For(core.Distro{Family: core.FamilyDebian})
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	if prov == nil {
		t.Fatal("provider nulo")
	}
}

func TestForUnsupported(t *testing.T) {
	_, err := For(core.Distro{Family: core.FamilyFedora})
	if !errors.Is(err, provider.ErrUnsupportedDistro) {
		t.Fatalf("err = %v, esperado ErrUnsupportedDistro", err)
	}
}
