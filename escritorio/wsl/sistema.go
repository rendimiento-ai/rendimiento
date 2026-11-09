package wsl

import "context"

// Sistema is the Ejecutor of this computer: on Windows, wsl.exe and the
// rest without a console window. Pendiente: for now it refuses everything.
func Sistema() Ejecutor { return sinSistema{} }

type sinSistema struct{}

func (sinSistema) Ejecutar(context.Context, []byte, string, ...string) ([]byte, error) {
	return nil, errPendiente
}

func (sinSistema) Iniciar(context.Context, string, ...string) (func(), error) {
	return nil, errPendiente
}

func (sinSistema) Elevar(context.Context, string, ...string) error { return errPendiente }
