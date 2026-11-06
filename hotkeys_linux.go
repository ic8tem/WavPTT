package main

import "golang.design/x/hotkey"

// Mod1 = Alt su X11.
var hotkeyMods = []hotkey.Modifier{hotkey.ModCtrl, hotkey.Mod1}

const hotkeyLabel = "Ctrl+Alt"

const hotkeyHint = " (servono una sessione X11 e nessun altro programma che li usi)"
