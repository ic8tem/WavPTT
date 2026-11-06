package core

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Tutti i dati stanno nella cartella del programma: WavPTT.ini e sottocartella WAV.
// Nulla viene scritto in AppData o nelle cartelle di sistema.

// Config: impostazioni salvate in WavPTT.ini.
type Config struct {
	TxMixer    string
	Port       string
	UseDTR     bool
	DelayMs    int
	Volume     int       // posizione del cursore 0..100 (logaritmico)
	FilterMode int       // 0 nessuno, 1 = 300-2700 Hz, 2 = 500-2400 Hz, 3 = 500-2100 Hz
	CompDB     int       // compressore: 0 = off, altrimenti 3..13 dB
	Memories   [8]string // percorso relativo alla cartella dati (es. WAV/cq.wav) o assoluto
}

// FilterNames: voci del menu filtro (indice = FilterMode).
var FilterNames = []string{"Nessun filtro", "300-2700 Hz", "500-2400 Hz", "500-2100 Hz"}

// MaxGain: amplificazione con il cursore tutto a destra.
const MaxGain = 1000.0

// GainOf: andamento logaritmico del cursore. 0 = silenzio, poi
// guadagno = 1000^(v/100): 1 ~ x1, 50 ~ x32, 100 = x1000 (passi uguali in dB).
func GainOf(vol int) float64 {
	if vol <= 0 {
		return 0
	}
	if vol > 100 {
		vol = 100
	}
	return math.Pow(MaxGain, float64(vol)/100.0)
}

// Opts converte le impostazioni nei parametri di lavorazione audio.
func (c Config) Opts() ProcOpts {
	return ProcOpts{Gain: GainOf(c.Volume), FilterMode: c.FilterMode, CompDB: c.CompDB}
}

// Store: posizione dei dati (cartella del programma).
type Store struct{ Dir string }

func (s Store) IniPath() string { return filepath.Join(s.Dir, "WavPTT.ini") }
func (s Store) WavDir() string  { return filepath.Join(s.Dir, "WAV") }

func ClampDelay(v int) int {
	if v < 0 {
		return 0
	}
	if v > 300 {
		return 300
	}
	return v
}

// Defaults: valori del primo avvio.
func Defaults() Config {
	return Config{DelayMs: 100, Volume: 100, FilterMode: 1, CompDB: 3}
}

func (s Store) Load() Config {
	c := Defaults()
	f, err := os.Open(s.IniPath())
	if err != nil {
		return c
	}
	defer f.Close()
	sec := ""
	sc := bufio.NewScanner(f)
	first := true
	for sc.Scan() {
		line := sc.Text()
		if first {
			line = strings.TrimPrefix(line, "\ufeff")
			first = false
		}
		line = strings.TrimSpace(line)
		if line == "" || line[0] == ';' || line[0] == '#' {
			continue
		}
		if line[0] == '[' && line[len(line)-1] == ']' {
			sec = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.ToLower(strings.TrimSpace(k))
		v = strings.TrimSpace(v)
		switch sec {
		case "impostazioni":
			switch k {
			case "txmixer":
				c.TxMixer = v
			case "porta":
				c.Port = v
			case "linea":
				c.UseDTR = strings.EqualFold(v, "DTR")
			case "ritardoms":
				if n, err := strconv.Atoi(v); err == nil {
					c.DelayMs = ClampDelay(n)
				}
			case "volume":
				if n, err := strconv.Atoi(v); err == nil && n >= 0 && n <= 100 {
					c.Volume = n
				}
			case "filtro":
				if n, err := strconv.Atoi(v); err == nil && n >= 0 && n <= 3 {
					c.FilterMode = n
				}
			case "compressore":
				if n, err := strconv.Atoi(v); err == nil {
					switch {
					case n == 1: // file della v9 (casella spuntata = 3 dB)
						c.CompDB = 3
					case n >= 3 && n <= 13:
						c.CompDB = n
					default:
						c.CompDB = 0
					}
				}
			}
		case "memorie":
			if len(k) == 2 && k[0] == 'f' && k[1] >= '1' && k[1] <= '8' {
				c.Memories[k[1]-'1'] = v
			}
		}
	}
	return c
}

func (s Store) Save(c Config) error {
	var b strings.Builder
	b.WriteString("; WavPTT - impostazioni (file nella cartella del programma)\r\n")
	b.WriteString("[Impostazioni]\r\n")
	fmt.Fprintf(&b, "TxMixer=%s\r\n", c.TxMixer)
	fmt.Fprintf(&b, "Porta=%s\r\n", c.Port)
	if c.UseDTR {
		b.WriteString("Linea=DTR\r\n")
	} else {
		b.WriteString("Linea=RTS\r\n")
	}
	fmt.Fprintf(&b, "RitardoMs=%d\r\n", ClampDelay(c.DelayMs))
	fmt.Fprintf(&b, "Volume=%d\r\n", c.Volume)
	fmt.Fprintf(&b, "Filtro=%d\r\n", c.FilterMode)
	fmt.Fprintf(&b, "Compressore=%d\r\n", c.CompDB)
	b.WriteString("\r\n[Memorie]\r\n")
	for i, m := range c.Memories {
		fmt.Fprintf(&b, "F%d=%s\r\n", i+1, m)
	}
	return os.WriteFile(s.IniPath(), []byte(b.String()), 0644)
}

// isRelStored: percorso relativo salvato nell'INI (accetta / e \ su ogni sistema).
func isRelStored(p string) bool {
	q := strings.ReplaceAll(p, `\`, "/")
	return strings.HasPrefix(strings.ToUpper(q), "WAV/")
}

// Abs restituisce il percorso assoluto di una memoria salvata.
func (s Store) Abs(p string) string {
	if p == "" {
		return ""
	}
	if isRelStored(p) {
		q := strings.ReplaceAll(p, `\`, "/")
		return filepath.Join(s.Dir, filepath.FromSlash(q))
	}
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(s.Dir, filepath.FromSlash(strings.ReplaceAll(p, `\`, "/")))
}

// Import copia i file scelti nella sottocartella WAV e restituisce i percorsi
// da salvare nell'INI (relativi, sempre con "/", es. WAV/cq.wav).
func (s Store) Import(src [8]string) ([8]string, error) {
	var out [8]string
	used := map[string]string{} // nome di destinazione (minuscolo) -> sorgente
	dir := s.WavDir()
	inWavDir := func(abs string) bool {
		return strings.EqualFold(filepath.Clean(filepath.Dir(abs)), filepath.Clean(dir))
	}
	// Prima i file che sono già nella cartella WAV: non vanno sovrascritti.
	for i, p := range src {
		if p == "" {
			continue
		}
		abs := s.Abs(p)
		if inWavDir(abs) {
			out[i] = "WAV/" + filepath.Base(abs)
			used[strings.ToLower(filepath.Base(abs))] = strings.ToLower(abs)
		}
	}
	for i, p := range src {
		if p == "" {
			continue
		}
		abs := s.Abs(p)
		if inWavDir(abs) {
			continue
		}
		if err := os.MkdirAll(dir, 0755); err != nil {
			return out, fmt.Errorf("impossibile creare la cartella WAV: %v", err)
		}
		name := filepath.Base(abs)
		ext := filepath.Ext(name)
		stem := strings.TrimSuffix(name, ext)
		for n := 2; ; n++ {
			prev, taken := used[strings.ToLower(name)]
			if !taken || prev == strings.ToLower(abs) {
				break
			}
			name = fmt.Sprintf("%s_%d%s", stem, n, ext)
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			return out, fmt.Errorf("impossibile leggere %s", filepath.Base(abs))
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
			return out, fmt.Errorf("impossibile copiare %s nella cartella WAV", name)
		}
		used[strings.ToLower(name)] = strings.ToLower(abs)
		out[i] = "WAV/" + name
	}
	return out, nil
}

// LoadPrepared legge il WAV e lo prepara per la scheda audio (8000 Hz, 16 bit).
func LoadPrepared(path string, c Config) (*PCM, error) {
	wv, err := LoadWav(path)
	if err != nil {
		return nil, err
	}
	out, err := Prepare(wv, OutRate, c.Opts())
	if err != nil {
		return nil, fmt.Errorf("%s: %v", filepath.Base(path), err)
	}
	return out, nil
}
