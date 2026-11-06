# WavPTT

Riproduttore di messaggi WAV per radioamatori, con comando del PTT su porta seriale (RTS o DTR).
Funziona su **Windows**, **Mac** (Apple Silicon e Intel) e **Linux**.

## Scaricare

Le versioni pronte sono nella pagina **Releases** del repository:

| Sistema | File |
|---|---|
| Windows | `WavPTT-windows.zip` |
| Mac | `WavPTT-mac.zip` (contiene `WavPTT.app`) |
| Linux | `WavPTT-linux-x86_64.tar.gz` |

Le istruzioni per il primo avvio su ogni sistema sono in `LEGGIMI.txt`, dentro ogni pacchetto.

## Funzioni

- 8 memorie, bottoni e tasti **F1…F8**; **Ctrl+Alt+F1…F8** anche da altri programmi (su Mac Ctrl+Option).
- Sequenza: apre la porta, alza **RTS** o **DTR**, attende il **ritardo** (0–300 ms), riproduce il WAV, rilascia il PTT.
- Memorie premute durante la trasmissione: in coda, 50 ms tra una e l'altra, PTT sempre attivo.
- Scheda audio **TX MIXER** impegnata a **8000 Hz**, 16 bit (su Windows tramite driver **MME**).
- Lavorazione dell'audio: compressore (off, 3…13 dB) → filtro passa banda (300–2700, 500–2400, 500–2100 Hz o nessuno) → volume logaritmico fino a ×1000 → limitazione dei picchi.
- **Esc** interrompe la trasmissione e rilascia il PTT.
- Tutti i dati (`WavPTT.ini` e cartella `WAV`) restano nella cartella del programma.

## Compilare

Le tre versioni vengono compilate automaticamente da GitHub Actions (`.github/workflows/build.yml`)
a ogni modifica; con un tag `vNN` vengono pubblicate nelle Releases.

Per compilare a mano serve Go 1.24 o successivo e un compilatore C:

```
go mod tidy
go build -ldflags "-H windowsgui" -o WavPTT.exe .   # Windows
go build -o WavPTT .                                 # Mac / Linux
```

Su Linux servono anche `libgl1-mesa-dev` e `xorg-dev`.
