package tci

import (
	"encoding/binary"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// fakeRadio simula ExpertSDR: manda "ready;", registra i comandi e, con
// trx attivo da sorgente tci, chiede audio con tx_chrono ogni 20 ms.
type fakeRadio struct {
	mu       sync.Mutex
	cmds     []string
	received []float32 // canale sinistro dei frame audio TX ricevuti
	format   uint32
	channels uint32
}

func (f *fakeRadio) handler(t *testing.T) http.HandlerFunc {
	up := websocket.Upgrader{}
	return func(w http.ResponseWriter, r *http.Request) {
		ws, err := up.Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer ws.Close()
		var wmu sync.Mutex
		send := func(mt int, b []byte) {
			wmu.Lock()
			defer wmu.Unlock()
			_ = ws.WriteMessage(mt, b)
		}
		send(websocket.TextMessage, []byte("protocol:ExpertSDR3,1.9;device:SunSDR2DX;ready;"))
		tx := make(chan bool, 4)
		go func() {
			var on bool
			tick := time.NewTicker(20 * time.Millisecond)
			defer tick.Stop()
			for {
				select {
				case v, ok := <-tx:
					if !ok {
						return
					}
					on = v
				case <-tick.C:
					if on {
						h := make([]byte, 64)
						binary.LittleEndian.PutUint32(h[4:], 8000)
						binary.LittleEndian.PutUint32(h[8:], f.format)
						binary.LittleEndian.PutUint32(h[20:], 160*f.channels) // 20 ms
						binary.LittleEndian.PutUint32(h[24:], typeTXChrono)
						binary.LittleEndian.PutUint32(h[28:], f.channels)
						send(websocket.BinaryMessage, h)
					}
				}
			}
		}()
		defer close(tx)
		for {
			mt, data, err := ws.ReadMessage()
			if err != nil {
				return
			}
			if mt == websocket.TextMessage {
				f.mu.Lock()
				f.cmds = append(f.cmds, string(data))
				f.mu.Unlock()
				switch string(data) {
				case "trx:0,true,tci;":
					tx <- true
				case "trx:0,false;":
					tx <- false
				}
				continue
			}
			if binary.LittleEndian.Uint32(data[24:]) != typeTXAudio {
				t.Errorf("tipo frame %d", binary.LittleEndian.Uint32(data[24:]))
			}
			n := int(binary.LittleEndian.Uint32(data[20:]))
			ch := int(f.channels)
			f.mu.Lock()
			for i := 0; i < n; i += ch {
				var v float32
				if f.format == 0 {
					v = float32(int16(binary.LittleEndian.Uint16(data[64+i*2:]))) / 32767
				} else {
					v = math.Float32frombits(binary.LittleEndian.Uint32(data[64+i*4:]))
				}
				f.received = append(f.received, v)
			}
			f.mu.Unlock()
		}
	}
}

func run(t *testing.T, format, channels uint32) {
	f := &fakeRadio{format: format, channels: channels}
	srv := httptest.NewServer(f.handler(t))
	defer srv.Close()
	addr := strings.TrimPrefix(srv.URL, "http://")

	c, err := Dial(addr, 8000)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.PTT(true); err != nil {
		t.Fatal(err)
	}
	rate, err := c.WaitRate(time.Second)
	if err != nil || rate != 8000 {
		t.Fatal(rate, err)
	}
	time.Sleep(60 * time.Millisecond) // ritardo PTT -> audio: silenzio
	sig := make([]float32, 4000)      // 0,5 s
	for i := range sig {
		sig[i] = float32(0.5 * math.Cos(2*math.Pi*1000*float64(i)/8000))
	}
	t0 := time.Now()
	if err := c.Play(sig, func() bool { return false }); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(t0); d < 500*time.Millisecond || d > 900*time.Millisecond {
		t.Errorf("durata %v", d)
	}
	c.Close()
	time.Sleep(50 * time.Millisecond)

	f.mu.Lock()
	defer f.mu.Unlock()
	cmds := strings.Join(f.cmds, "")
	for _, want := range []string{"audio_samplerate:8000;", "audio_start:0;", "trx:0,true,tci;", "trx:0,false;", "audio_stop:0;"} {
		if !strings.Contains(cmds, want) {
			t.Errorf("manca %s in %s", want, cmds)
		}
	}
	// l'audio ricevuto contiene il segnale intero, dopo il silenzio iniziale
	start := -1
	for i, v := range f.received {
		if v != 0 {
			start = i
			break
		}
	}
	if start < 0 || len(f.received)-start < len(sig) {
		t.Fatalf("audio ricevuto %d campioni, inizio %d", len(f.received), start)
	}
	tol := float32(1e-6)
	if format == 0 {
		tol = 1e-4
	}
	for i := range sig {
		if d := f.received[start+i] - sig[i]; d > tol || d < -tol {
			t.Fatalf("campione %d: %v invece di %v", i, f.received[start+i], sig[i])
		}
	}
}

func TestTXFloatStereo(t *testing.T) { run(t, 3, 2) }
func TestTXInt16Mono(t *testing.T)   { run(t, 0, 1) }

func TestStopAndNoServer(t *testing.T) {
	if _, err := Dial("127.0.0.1:1", 8000); err == nil {
		t.Fatal("collegamento a porta chiusa riuscito")
	}
	f := &fakeRadio{format: 3, channels: 2}
	srv := httptest.NewServer(f.handler(t))
	defer srv.Close()
	c, err := Dial(strings.TrimPrefix(srv.URL, "http://"), 8000)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.PTT(true)
	c.WaitRate(time.Second)
	sig := make([]float32, 80000) // 10 s
	t0 := time.Now()
	err = c.Play(sig, func() bool { return time.Since(t0) > 100*time.Millisecond })
	if err != nil || time.Since(t0) > 300*time.Millisecond {
		t.Fatal("stop non rispettato", err, time.Since(t0))
	}
}
