package main

import (
	"os"
	"path/filepath"
	"strings"
)

// dataDir: cartella del programma, dove stanno WavPTT.ini e la cartella WAV.
// Su Mac è la cartella che contiene WavPTT.app (non l'interno del pacchetto).
func dataDir() string {
	p, err := os.Executable()
	if err != nil {
		return "."
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	dir := filepath.Dir(p)
	if i := strings.Index(dir, ".app/Contents/MacOS"); i >= 0 {
		return filepath.Dir(dir[:i+len(".app")])
	}
	return dir
}
