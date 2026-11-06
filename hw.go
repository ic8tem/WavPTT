package main

import (
	"errors"
	"fmt"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gen2brain/malgo"
	"go.bug.st/serial"

	"wavptt/core"
)

// hw: porta seriale (PTT) e scheda audio; implementa core.Backend.
type hw struct {
	mu  sync.Mutex
	ctx *malgo.AllocatedContext
}

// newHW apre il sistema audio. Su Windows si usa solo il driver MME (WinMM).
func newHW() (*hw, error) {
	var backends []malgo.Backend
	if runtime.GOOS == "windows" {
		backends = []malgo.Backend{malgo.BackendWinmm}
	}
	ctx, err := malgo.InitContext(backends, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, fmt.Errorf("sistema audio non disponibile: %v", err)
	}
	return &hw{ctx: ctx}, nil
}

func (h *hw) close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.ctx != nil {
		_ = h.ctx.Uninit()
		h.ctx.Free()
		h.ctx = nil
	}
}

// playbackDevices: schede audio di uscita, con nomi resi unici.
func (h *hw) playbackDevices() ([]string, []malgo.DeviceInfo) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.ctx == nil {
		return nil, nil
	}
	infos, err := h.ctx.Devices(malgo.Playback)
	if err != nil {
		return nil, nil
	}
	seen := map[string]int{}
	names := make([]string, len(infos))
	for i := range infos {
		n := strings.TrimSpace(infos[i].Name())
		seen[n]++
		if seen[n] > 1 {
			n = fmt.Sprintf("%s (%d)", n, seen[n])
		}
		names[i] = n
	}
	return names, infos
}

func (h *hw) DeviceNames() []string {
	n, _ := h.playbackDevices()
	return n
}

// Play riproduce PCM 16 bit sulla scheda indicata ("" = predefinita) e ritorna
// quando l'audio è uscito dalla scheda oppure quando stop() diventa vero.
func (h *hw) Play(pcm *core.PCM, device string, stop func() bool) error {
	if pcm == nil || len(pcm.Data) == 0 {
		return errors.New("audio vuoto")
	}
	cfg := malgo.DefaultDeviceConfig(malgo.Playback)
	cfg.Playback.Format = malgo.FormatS16
	cfg.Playback.Channels = uint32(pcm.Channels)
	cfg.SampleRate = uint32(pcm.Rate)
	cfg.PeriodSizeInMilliseconds = 20
	cfg.Periods = 3
	if device != "" {
		names, infos := h.playbackDevices()
		found := false
		for i, n := range names {
			if strings.EqualFold(n, device) {
				cfg.Playback.DeviceID = infos[i].ID.Pointer()
				found = true
				break
			}
		}
		if !found {
			return errors.New("TX MIXER: scheda audio non trovata - controlla la scelta")
		}
	}

	bpf := pcm.Channels * 2 // byte per frame
	// dopo la fine del file si continua a mandare silenzio finché i buffer
	// della scheda (3 x 20 ms) sono stati suonati: così l'audio non viene
	// tagliato quando si abbassa il PTT.
	drainBytes := pcm.Rate * bpf * 80 / 1000
	var pos, silence int
	var finished atomic.Bool
	done := make(chan struct{})
	signal := func() {
		if finished.CompareAndSwap(false, true) {
			close(done)
		}
	}
	onData := func(out, _ []byte, frames uint32) {
		n := int(frames) * bpf
		if n > len(out) {
			n = len(out)
		}
		if finished.Load() || stop() {
			clear(out[:n])
			signal()
			return
		}
		c := copy(out[:n], pcm.Data[pos:])
		pos += c
		if c < n {
			clear(out[c:n])
			silence += n - c
			if silence >= drainBytes {
				signal()
			}
		}
	}

	h.mu.Lock()
	ctx := h.ctx
	h.mu.Unlock()
	if ctx == nil {
		return errors.New("sistema audio non disponibile")
	}
	dev, err := malgo.InitDevice(ctx.Context, cfg, malgo.DeviceCallbacks{Data: onData})
	if err != nil {
		return fmt.Errorf("la scheda audio non si apre a %d Hz: %v", pcm.Rate, err)
	}
	defer dev.Uninit()
	if err := dev.Start(); err != nil {
		return fmt.Errorf("la scheda audio non parte: %v", err)
	}
	dur := time.Duration(len(pcm.Data)/bpf) * time.Second / time.Duration(pcm.Rate)
	select {
	case <-done:
	case <-time.After(dur + 3*time.Second):
		return errors.New("la scheda audio non risponde")
	}
	return nil
}

// PTTOn apre la porta seriale con RTS e DTR bassi, poi alza la linea scelta.
func (h *hw) PTTOn(port string, useDTR bool) (func(), error) {
	if port == "" {
		return nil, errors.New("nessuna porta COM selezionata")
	}
	p, err := serial.Open(port, &serial.Mode{
		BaudRate:          9600,
		InitialStatusBits: &serial.ModemOutputBits{RTS: false, DTR: false},
	})
	if err != nil {
		return nil, fmt.Errorf("impossibile aprire %s (occupata o inesistente)", port)
	}
	if useDTR {
		_ = p.SetRTS(false)
		err = p.SetDTR(true)
	} else {
		_ = p.SetDTR(false)
		err = p.SetRTS(true)
	}
	if err != nil {
		p.Close()
		return nil, fmt.Errorf("impossibile attivare la linea su %s", port)
	}
	return func() {
		if useDTR {
			_ = p.SetDTR(false)
		} else {
			_ = p.SetRTS(false)
		}
		p.Close()
	}, nil
}

// comPorts: porte seriali presenti, ordinate (COM1, COM2, ... / /dev/...).
func comPorts() []string {
	list, err := serial.GetPortsList()
	if err != nil {
		return nil
	}
	var out []string
	for _, p := range list {
		// su Mac ogni porta compare due volte: si usano le /dev/cu.*
		if runtime.GOOS == "darwin" && strings.HasPrefix(p, "/dev/tty.") {
			continue
		}
		out = append(out, p)
	}
	sort.Slice(out, func(a, b int) bool {
		na, nb := portNum(out[a]), portNum(out[b])
		if na != nb {
			return na < nb
		}
		return out[a] < out[b]
	})
	return out
}

func portNum(s string) int {
	u := strings.ToUpper(s)
	if strings.HasPrefix(u, "COM") {
		if n, err := strconv.Atoi(u[3:]); err == nil {
			return n
		}
	}
	return 1 << 20
}
