package core

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func writeWav(t *testing.T, path string, rate int, samples []int16) {
	t.Helper()
	data := make([]byte, len(samples)*2)
	for i, s := range samples {
		binary.LittleEndian.PutUint16(data[i*2:], uint16(s))
	}
	h := make([]byte, 44)
	copy(h[0:], "RIFF")
	binary.LittleEndian.PutUint32(h[4:], uint32(36+len(data)))
	copy(h[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(h[16:], 16)
	binary.LittleEndian.PutUint16(h[20:], 1)
	binary.LittleEndian.PutUint16(h[22:], 1)
	binary.LittleEndian.PutUint32(h[24:], uint32(rate))
	binary.LittleEndian.PutUint32(h[28:], uint32(rate*2))
	binary.LittleEndian.PutUint16(h[32:], 2)
	binary.LittleEndian.PutUint16(h[34:], 16)
	copy(h[36:], "data")
	binary.LittleEndian.PutUint32(h[40:], uint32(len(data)))
	if err := os.WriteFile(path, append(h, data...), 0644); err != nil {
		t.Fatal(err)
	}
}

func tone(rate int, f float64, amp float64, n int) []int16 {
	s := make([]int16, n)
	for i := range s {
		s[i] = int16(amp * 32767 * math.Sin(2*math.Pi*f*float64(i)/float64(rate)))
	}
	return s
}

func peak16(d []byte) float64 {
	p := 0.0
	for i := 0; i+1 < len(d); i += 2 {
		p = math.Max(p, math.Abs(float64(int16(binary.LittleEndian.Uint16(d[i:])))))
	}
	return p
}

func TestPrepareAndFilters(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.wav")
	writeWav(t, p, 44100, tone(44100, 1000, 0.001, 44100))
	wv, err := LoadWav(p)
	if err != nil {
		t.Fatal(err)
	}
	pcm, err := Prepare(wv, OutRate, ProcOpts{Gain: 100, FilterMode: 1})
	if err != nil {
		t.Fatal(err)
	}
	if pcm.Rate != 8000 || pcm.Channels != 1 || len(pcm.Data) != 8000*2 {
		t.Fatalf("formato errato %d %d %d", pcm.Rate, pcm.Channels, len(pcm.Data))
	}
	// 0.001 * 100 = 0.1 di fondo scala
	if pk := peak16(pcm.Data[4000:]) / 32768; math.Abs(pk-0.1) > 0.01 {
		t.Fatalf("guadagno errato: %.3f", pk)
	}
	// limitazione dei picchi
	pcm, _ = Prepare(wv, OutRate, ProcOpts{Gain: 5000})
	if pk := peak16(pcm.Data); pk < 32766 {
		t.Fatalf("limitatore: %.0f", pk)
	}
	// risposta dei filtri a 8000 Hz
	type pt struct {
		mode    int
		f, want float64
	}
	for _, c := range []pt{{1, 1000, 0}, {1, 300, -3}, {1, 2700, -3}, {2, 500, -3}, {2, 2400, -3}, {3, 500, -3}, {3, 2100, -3}, {3, 2700, -17.5}} {
		x := make([]float64, 16000)
		for i := range x {
			x[i] = math.Sin(2 * math.Pi * c.f * float64(i) / 8000)
		}
		lo, hi := map[int][2]float64{1: {300, 2700}, 2: {500, 2400}, 3: {500, 2100}}[c.mode][0], map[int][2]float64{1: {300, 2700}, 2: {500, 2400}, 3: {500, 2100}}[c.mode][1]
		bandPass(x, 8000, lo, hi)
		pk := 0.0
		for _, v := range x[8000:] {
			pk = math.Max(pk, math.Abs(v))
		}
		if db := 20 * math.Log10(pk); math.Abs(db-c.want) > 0.6 {
			t.Errorf("filtro %d a %.0f Hz: %.1f dB (atteso %.1f)", c.mode, c.f, db, c.want)
		}
	}
}

func TestCompressor(t *testing.T) {
	for _, db := range []float64{3, 8, 13} {
		x := make([]float64, 16000)
		for i := range x {
			a := 0.8
			if i < 8000 {
				a = 0.01
			}
			x[i] = a * math.Sin(2*math.Pi*1000*float64(i)/8000)
		}
		compress([][]float64{x}, 8000, db)
		pk := func(s []float64) float64 {
			p := 0.0
			for _, v := range s {
				p = math.Max(p, math.Abs(v))
			}
			return p
		}
		weak := 20 * math.Log10(pk(x[2000:7000])/0.01)
		strong := 20 * math.Log10(pk(x[10000:])/0.8)
		if math.Abs(weak-db) > 0.2 || math.Abs(strong) > 0.2 {
			t.Errorf("compressore %.0f dB: debole %+.1f forte %+.1f", db, weak, strong)
		}
	}
}

func TestGainCurve(t *testing.T) {
	if GainOf(0) != 0 || math.Abs(GainOf(100)-1000) > 1e-9 || math.Abs(GainOf(50)-31.62) > 0.01 {
		t.Fatal(GainOf(0), GainOf(50), GainOf(100))
	}
}

func TestConfigAndImport(t *testing.T) {
	st := Store{Dir: t.TempDir()}
	src := t.TempDir()
	a := filepath.Join(src, "cq.wav")
	sub := filepath.Join(src, "x")
	os.MkdirAll(sub, 0755)
	b := filepath.Join(sub, "cq.wav")
	writeWav(t, a, 8000, tone(8000, 500, 0.5, 800))
	writeWav(t, b, 8000, tone(8000, 700, 0.5, 800))
	var m [8]string
	m[0], m[2] = a, b
	out, err := st.Import(m)
	if err != nil {
		t.Fatal(err)
	}
	if out[0] != "WAV/cq.wav" || out[2] != "WAV/cq_2.wav" {
		t.Fatalf("%v", out)
	}
	c := Config{TxMixer: "USB Audio CODEC", Port: "COM7", UseDTR: true, DelayMs: 120, Volume: 37, FilterMode: 3, CompDB: 9, Memories: out, TCIAddr: "192.168.1.20:50001", Theme: "Viola"}
	if err := st.Save(c); err != nil {
		t.Fatal(err)
	}
	if got := st.Load(); got != c {
		t.Fatalf("%+v", got)
	}
	// INI scritto da Windows con "\" funziona anche su Mac/Linux
	if p := st.Abs(`WAV\cq.wav`); p != filepath.Join(st.Dir, "WAV", "cq.wav") {
		t.Fatal(p)
	}
	// reimport dei file già in WAV: nessuna copia doppia
	again, _ := st.Import([8]string{out[0], "", out[2]})
	if again[0] != out[0] || again[2] != out[2] {
		t.Fatal(again)
	}
	ents, _ := os.ReadDir(st.WavDir())
	if len(ents) != 2 {
		t.Fatal(len(ents))
	}
}

type ev struct {
	t    time.Duration
	what string
}

type fakeBackend struct {
	mu      sync.Mutex
	t0      time.Time
	events  []ev
	playMs  int
	failPTT bool
}

func (f *fakeBackend) log(s string) {
	f.mu.Lock()
	f.events = append(f.events, ev{time.Since(f.t0), s})
	f.mu.Unlock()
}

type fakeTX struct {
	f   *fakeBackend
	dev string
}

func (t fakeTX) Release()                            { t.f.log("OFF") }
func (t fakeTX) Play(p *PCM, stop func() bool) error { return t.f.Play(p, t.dev, stop) }

func (f *fakeBackend) PTTOn(c Config) (Transmission, error) {
	if f.failPTT {
		return nil, os.ErrNotExist
	}
	l, port := "RTS", c.Port
	if c.UseDTR {
		l = "DTR"
	}
	if c.UseTCI {
		l, port = "TCI", c.TCIAddr
	}
	f.log("ON " + port + " " + l)
	return fakeTX{f, c.TxMixer}, nil
}
func (f *fakeBackend) Play(p *PCM, dev string, stop func() bool) error {
	f.log("PLAY " + string(p.Data) + " " + dev)
	for i := 0; i < f.playMs; i++ {
		if stop() {
			f.log("STOPPED")
			return nil
		}
		time.Sleep(time.Millisecond)
	}
	f.log("END")
	return nil
}

func TestEngineQueue(t *testing.T) {
	fb := &fakeBackend{t0: time.Now(), playMs: 100}
	cfg := Config{Port: "COM3", DelayMs: 80, TxMixer: "Mixer", Memories: [8]string{"/m/F1", "", "/m/F3"}}
	var mu sync.Mutex
	var statuses []string
	e := NewEngine(fb, Store{Dir: "/"}, func() Config { return cfg },
		func(k int, s string) { mu.Lock(); statuses = append(statuses, s); mu.Unlock() }, func(int) {})
	e.Load = func(p string, c Config) (*PCM, error) { return &PCM{Data: []byte(filepath.Base(p))}, nil }
	go e.Run()
	if e.Enqueue(1) {
		t.Fatal("memoria vuota accettata")
	}
	e.Enqueue(0)
	time.Sleep(20 * time.Millisecond)
	e.Enqueue(2) // premuta durante la trasmissione
	time.Sleep(500 * time.Millisecond)
	fb.mu.Lock()
	evs := fb.events
	fb.mu.Unlock()
	var seq []string
	for _, x := range evs {
		seq = append(seq, x.what)
	}
	want := "ON COM3 RTS|PLAY F1 Mixer|END|PLAY F3 Mixer|END|OFF"
	if strings.Join(seq, "|") != want {
		t.Fatalf("sequenza: %v", seq)
	}
	if d := evs[1].t - evs[0].t; d < 78*time.Millisecond || d > 110*time.Millisecond {
		t.Errorf("ritardo PTT->audio %v", d)
	}
	if d := evs[3].t - evs[2].t; d < 49*time.Millisecond || d > 75*time.Millisecond {
		t.Errorf("pausa tra memorie %v", d)
	}
	if evs[5].t-evs[4].t > 10*time.Millisecond {
		t.Errorf("rilascio PTT ritardato %v", evs[5].t-evs[4].t)
	}

	// Esc durante la trasmissione: audio fermato, coda svuotata, PTT rilasciato
	fb.mu.Lock()
	fb.events = nil
	fb.mu.Unlock()
	cfg.UseDTR = true
	e.Enqueue(0)
	e.Enqueue(2)
	time.Sleep(110 * time.Millisecond)
	e.Stop()
	time.Sleep(100 * time.Millisecond)
	seq = nil
	for _, x := range fb.events {
		seq = append(seq, x.what)
	}
	if strings.Join(seq, "|") != "ON COM3 DTR|PLAY F1 Mixer|STOPPED|OFF" {
		t.Fatalf("stop: %v", seq)
	}
	if e.Busy() {
		t.Fatal("ancora occupato")
	}

	// PTT tramite TCI
	fb.mu.Lock()
	fb.events = nil
	fb.mu.Unlock()
	cfg.UseTCI, cfg.TCIAddr = true, "127.0.0.1:40001"
	e.Enqueue(0)
	time.Sleep(250 * time.Millisecond)
	seq = nil
	for _, x := range fb.events {
		seq = append(seq, x.what)
	}
	if strings.Join(seq, "|") != "ON 127.0.0.1:40001 TCI|PLAY F1 Mixer|END|OFF" {
		t.Fatalf("tci: %v", seq)
	}

	// porta COM non apribile: niente audio
	fb.failPTT = true
	e.Enqueue(0)
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	last := statuses[len(statuses)-1]
	mu.Unlock()
	if !strings.HasPrefix(last, "PTT:") {
		t.Fatal(last)
	}
}
