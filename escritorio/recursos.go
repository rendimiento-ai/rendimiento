package main

import "embed"

// The assistant's page, served to the window by Wails.
//
//go:embed frontend
var pagina embed.FS

// The icon next to the clock (made once by icono.py).
//
//go:embed rendimiento.ico
var icono []byte

// version is the release this was built from (-X main.version=v0.1.8).
var version = "dev"
