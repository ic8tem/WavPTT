package core

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
)

// WavData: file WAV in memoria (formato WAVEFORMATEX + campioni).
type WavData struct {
	Format []byte
	Data   []byte
}

// PCM: audio pronto per la scheda (16 bit, interlacciato).
type PCM struct {
	Data     []byte
	Channels int
	Rate     int
}

// LoadWav legge un file WAV.
func LoadWav(path string) (*WavData, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("impossibile leggere %s", filepath.Base(path))
	}
	if len(b) < 12 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return nil, fmt.Errorf("%s non è un file WAV valido", filepath.Base(path))
	}
	var fmtChunk, dataChunk []byte
	pos := 12
	for pos+8 <= len(b) {
		id := string(b[pos : pos+4])
		size := int(binary.LittleEndian.Uint32(b[pos+4 : pos+8]))
		start := pos + 8
		end := start + size
		if end > len(b) || size < 0 {
			end = len(b) // file troncato: usa quello che c'è
		}
		switch id {
		case "fmt ":
			fmtChunk = b[start:end]
		case "data":
			dataChunk = b[start:end]
		}
		pos = start + size
		if size%2 == 1 {
			pos++
		}
	}
	if len(fmtChunk) < 16 || dataChunk == nil {
		return nil, fmt.Errorf("%s: formato WAV non riconosciuto", filepath.Base(path))
	}
	f := make([]byte, len(fmtChunk))
	copy(f, fmtChunk)
	if len(f) < 18 {
		f = append(f, 0, 0) // cbSize = 0 per PCM semplice
	}
	blockAlign := int(binary.LittleEndian.Uint16(f[12:14]))
	d := dataChunk
	if blockAlign > 0 {
		d = d[:len(d)-len(d)%blockAlign]
	}
	if len(d) == 0 {
		return nil, fmt.Errorf("%s non contiene audio", filepath.Base(path))
	}
	return &WavData{Format: f, Data: d}, nil
}

// OutRate: frequenza di campionamento con cui si impegna la scheda audio.
const OutRate = 8000

// decode converte i campioni in float (-1..1), un canale per riga.
func decode(wv *WavData) ([][]float64, int, error) {
	f := wv.Format
	tag := binary.LittleEndian.Uint16(f[0:2])
	ch := int(binary.LittleEndian.Uint16(f[2:4]))
	rate := int(binary.LittleEndian.Uint32(f[4:8]))
	bits := int(binary.LittleEndian.Uint16(f[14:16]))
	if tag == 0xFFFE && len(f) >= 26 {
		tag = binary.LittleEndian.Uint16(f[24:26]) // sottoformato
	}
	if ch < 1 || ch > 8 || rate < 1000 || rate > 384000 {
		return nil, 0, errors.New("formato WAV non gestito")
	}
	bps := bits / 8
	if bps < 1 || (tag != 1 && tag != 3) || (tag == 3 && bps != 4 && bps != 8) || (tag == 1 && bps > 4) {
		return nil, 0, fmt.Errorf("formato WAV non gestito (tipo %d, %d bit)", tag, bits)
	}
	n := len(wv.Data) / (bps * ch)
	out := make([][]float64, ch)
	for c := range out {
		out[c] = make([]float64, n)
	}
	d := wv.Data
	for i := 0; i < n; i++ {
		for c := 0; c < ch; c++ {
			p := (i*ch + c) * bps
			var v float64
			switch {
			case tag == 3 && bps == 4:
				v = float64(math.Float32frombits(binary.LittleEndian.Uint32(d[p:])))
			case tag == 3:
				v = math.Float64frombits(binary.LittleEndian.Uint64(d[p:]))
			case bps == 1:
				v = (float64(d[p]) - 128) / 128
			case bps == 2:
				v = float64(int16(binary.LittleEndian.Uint16(d[p:]))) / 32768
			case bps == 3:
				x := int32(uint32(d[p]) | uint32(d[p+1])<<8 | uint32(d[p+2])<<16)
				v = float64((x<<8)>>8) / 8388608
			default:
				v = float64(int32(binary.LittleEndian.Uint32(d[p:]))) / 2147483648
			}
			out[c][i] = v
		}
	}
	return out, rate, nil
}

const kRes = 1024 // punti della tabella per ogni attraversamento dello zero

var kTable = func() []float64 {
	const zc = 16
	t := make([]float64, zc*kRes+2)
	for i := range t {
		a := float64(i) / kRes
		v := 1.0
		if a != 0 {
			v = math.Sin(math.Pi*a) / (math.Pi * a)
		}
		if a < zc {
			v *= 0.5 + 0.5*math.Cos(math.Pi*a/zc) // finestra di Hann
		} else {
			v = 0
		}
		t[i] = v
	}
	return t
}()

// kernelAt: sinc a finestra, interpolato dalla tabella (a in attraversamenti dello zero).
func kernelAt(a float64) float64 {
	if a < 0 {
		a = -a
	}
	f := a * kRes
	i := int(f)
	if i >= len(kTable)-1 {
		return 0
	}
	fr := f - float64(i)
	return kTable[i] + (kTable[i+1]-kTable[i])*fr
}

// resample converte un canale da inRate a outRate con filtro sinc a finestra
// (passa-basso automatico quando si scende di frequenza).
func resample(x []float64, inRate, outRate int) []float64 {
	if inRate == outRate || len(x) == 0 {
		return x
	}
	ratio := float64(outRate) / float64(inRate)
	cut := math.Min(1, ratio) * 0.95
	const zc = 16 // attraversamenti dello zero per lato
	half := float64(zc) / cut
	n := int(float64(len(x)) * ratio)
	y := make([]float64, n)
	for i := 0; i < n; i++ {
		t := float64(i) / ratio
		lo := int(math.Ceil(t - half))
		hi := int(math.Floor(t + half))
		if lo < 0 {
			lo = 0
		}
		if hi > len(x)-1 {
			hi = len(x) - 1
		}
		var acc float64
		for k := lo; k <= hi; k++ {
			acc += x[k] * kernelAt((float64(k)-t)*cut)
		}
		acc *= cut
		y[i] = acc
	}
	return y
}

// ProcOpts: parametri di lavorazione.
type ProcOpts struct {
	Gain       float64
	FilterMode int // 0 nessuno, 1 = 300-2700, 2 = 500-2400, 3 = 500-2100
	CompDB     int // 0 = off, 3..13 dB
}

// Prepare porta l'audio a rate Hz, 16 bit PCM.
// Schema: conversione di frequenza, compressore, filtro, amplificazione, limitazione dei picchi.
func Prepare(wv *WavData, rate int, o ProcOpts) (*PCM, error) {
	gain := o.Gain
	chans, inRate, err := decode(wv)
	if err != nil {
		return nil, err
	}
	// schema: conversione a 8000 Hz, compressore, filtro, amplificazione, limitazione
	for c := range chans {
		chans[c] = resample(chans[c], inRate, rate)
	}
	if o.CompDB > 0 {
		compress(chans, float64(rate), float64(o.CompDB))
	}
	for c := range chans {
		switch o.FilterMode {
		case 1:
			bandPass(chans[c], float64(rate), 300, 2700)
		case 2:
			bandPass(chans[c], float64(rate), 500, 2400)
		case 3:
			bandPass(chans[c], float64(rate), 500, 2100)
		}
	}
	ch := len(chans)
	n := len(chans[0])
	if n == 0 {
		return nil, errors.New("audio vuoto")
	}
	data := make([]byte, n*ch*2)
	for i := 0; i < n; i++ {
		for c := 0; c < ch; c++ {
			v := chans[c][i] * gain * 32768
			if v > 32767 {
				v = 32767
			} else if v < -32768 {
				v = -32768
			}
			binary.LittleEndian.PutUint16(data[(i*ch+c)*2:], uint16(int16(math.Round(v))))
		}
	}
	return &PCM{Data: data, Channels: ch, Rate: rate}, nil
}

// biquad: filtro del secondo ordine (forma diretta I).
type biquad struct{ b0, b1, b2, a1, a2, x1, x2, y1, y2 float64 }

func (q *biquad) run(x []float64) {
	for i, v := range x {
		y := q.b0*v + q.b1*q.x1 + q.b2*q.x2 - q.a1*q.y1 - q.a2*q.y2
		q.x2, q.x1 = q.x1, v
		q.y2, q.y1 = q.y1, y
		x[i] = y
	}
}

// newBiquad: passa-alto o passa-basso (formule RBJ) con frequenza f0 e fattore Q.
func newBiquad(highpass bool, fs, f0, Q float64) *biquad {
	w0 := 2 * math.Pi * f0 / fs
	cw, alpha := math.Cos(w0), math.Sin(w0)/(2*Q)
	a0 := 1 + alpha
	q := &biquad{a1: -2 * cw / a0, a2: (1 - alpha) / a0}
	if highpass {
		q.b0 = (1 + cw) / 2 / a0
		q.b1 = -(1 + cw) / a0
	} else {
		q.b0 = (1 - cw) / 2 / a0
		q.b1 = (1 - cw) / a0
	}
	q.b2 = q.b0
	return q
}

// bandPass: passa-alto lo + passa-basso hi, Butterworth del 4 ordine ciascuno.
func bandPass(x []float64, fs, lo, hi float64) {
	for _, Q := range []float64{0.5411961, 1.3065630} {
		newBiquad(true, fs, lo, Q).run(x)
		newBiquad(false, fs, hi, Q).run(x)
	}
}

// compress: compressore 2:1 con soglia 2*db sotto il picco dell'inviluppo,
// quindi i picchi più forti vengono ridotti di db dB; poi +db dB di guadagno di
// recupero. Inviluppo comune a tutti i canali (attacco 5 ms, rilascio 150 ms).
func compress(chans [][]float64, fs, db float64) {
	n := len(chans[0])
	if n == 0 {
		return
	}
	atk := math.Exp(-1 / (0.005 * fs))
	rel := math.Exp(-1 / (0.150 * fs))
	env := make([]float64, n)
	e, emax := 0.0, 0.0
	for i := 0; i < n; i++ {
		a := 0.0
		for _, ch := range chans {
			a = math.Max(a, math.Abs(ch[i]))
		}
		if a > e {
			e = atk*e + (1-atk)*a
		} else {
			e = rel*e + (1-rel)*a
		}
		env[i] = e
		emax = math.Max(emax, e)
	}
	if emax <= 0 {
		return
	}
	thr := emax * math.Pow(10, -2*db/20) // soglia: 2*db sotto il picco
	makeup := math.Pow(10, db/20)        // recupero: +db dB
	for i := 0; i < n; i++ {
		g := makeup
		if env[i] > thr {
			over := 20 * math.Log10(env[i]/thr) // dB sopra la soglia
			g *= math.Pow(10, -0.5*over/20)     // rapporto 2:1
		}
		for _, ch := range chans {
			ch[i] *= g
		}
	}
}

// Resample converte un segnale da inRate a outRate (filtro sinc a finestra).
func Resample(x []float64, inRate, outRate int) []float64 { return resample(x, inRate, outRate) }
