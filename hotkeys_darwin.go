package main

import "golang.design/x/hotkey"

// Su Mac il tasto Alt si chiama Option.
var hotkeyMods = []hotkey.Modifier{hotkey.ModCtrl, hotkey.ModOption}

const hotkeyLabel = "Ctrl+Option"

const hotkeyHint = " (concedi a WavPTT l'accesso in Impostazioni di Sistema > Privacy e sicurezza > Accessibilità)"
