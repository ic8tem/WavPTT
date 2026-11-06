package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.design/x/hotkey"
)

var fKeys = []hotkey.Key{hotkey.KeyF1, hotkey.KeyF2, hotkey.KeyF3, hotkey.KeyF4,
	hotkey.KeyF5, hotkey.KeyF6, hotkey.KeyF7, hotkey.KeyF8}

// registerGlobalHotkeys registra Ctrl+Alt+F1..F8 (su Mac Ctrl+Option+F1..F8).
// Se anche una sola combinazione non è disponibile le libera tutte e
// restituisce il testo dell'avviso: i comandi restano disattivati fino alla chiusura.
func registerGlobalHotkeys(fire func(i int)) (release func(), warn string) {
	var ok []*hotkey.Hotkey
	var busy []string
	for i, k := range fKeys {
		hk := hotkey.New(hotkeyMods, k)
		if err := registerWithTimeout(hk); err != nil {
			busy = append(busy, fmt.Sprintf("F%d", i+1))
			continue
		}
		ok = append(ok, hk)
		go func(i int, hk *hotkey.Hotkey) {
			for range hk.Keydown() {
				fire(i)
			}
		}(i, hk)
	}
	release = func() {
		for _, hk := range ok {
			_ = hk.Unregister()
		}
	}
	if len(busy) == len(fKeys) {
		release()
		return func() {}, "comandi " + hotkeyLabel + "+F1..F8 non disponibili su questo computer" + hotkeyHint +
			": restano disattivati fino alla chiusura"
	}
	if len(busy) > 0 {
		release()
		return func() {}, hotkeyLabel + "+" + strings.Join(busy, ", ") +
			" bloccati da un altro programma: comandi " + hotkeyLabel + " disattivati fino alla chiusura"
	}
	return release, ""
}

func registerWithTimeout(hk *hotkey.Hotkey) error {
	ch := make(chan error, 1)
	go func() { ch <- hk.Register() }()
	select {
	case err := <-ch:
		return err
	case <-time.After(3 * time.Second):
		return errors.New("timeout")
	}
}
