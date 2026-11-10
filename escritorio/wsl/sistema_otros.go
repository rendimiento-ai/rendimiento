//go:build !windows

package wsl

import (
	"context"
	"errors"
)

var errSoloWindows = errors.New("wsl: this only works on Windows")

// Sistema is the Ejecutor of this computer. WSL is Windows': anywhere
// else it refuses everything.
func Sistema() Ejecutor { return sinSistema{} }

type sinSistema struct{}

func (sinSistema) Ejecutar(context.Context, []byte, string, ...string) ([]byte, error) {
	return nil, errSoloWindows
}

func (sinSistema) Iniciar(context.Context, string, ...string) (func(), error) {
	return nil, errSoloWindows
}

func (sinSistema) Elevar(context.Context, string, ...string) error { return errSoloWindows }
