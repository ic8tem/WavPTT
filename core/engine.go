package core

import (
	"fmt"
	"path/filepath"
	"sync/atomic"
	"time"
)

// Stato mostrato nella riga di stato.
const (
	StatusNormal = 0
	StatusTX     = 1
	StatusError  = 2
)

// Backend: hardware (porta COM e scheda audio). Separato per poterlo provare.
type Backend interface {
	// PTTOn attiva il PTT secondo le impostazioni: porta seriale (alza RTS o
	// DTR, l'altra linea resta bassa) oppure TCI. Restituisce la funzione che
	// rilascia il PTT e il riproduttore da usare per l'audio di questa trasmissione.
	PTTOn(c Config) (tx Transmission, err error)
	// Play riproduce sulla scheda audio indicata ("" = predefinita), senza PTT
	// (ascolto di prova).
	Play(pcm *PCM, device string, stop func() bool) error
}

// Transmission: una trasmissione in corso (PTT attivo).
type Transmission interface {
	// Play riproduce e ritorna quando l'audio è uscito o stop() è vero.
	Play(pcm *PCM, stop func() bool) error
	// Release rilascia il PTT.
	Release()
}

// Engine: coda delle memorie e sequenza PTT -> ritardo -> audio -> rilascio.
type Engine struct {
	B       Backend
	Store   Store
	Cfg     func() Config
	Status  func(kind int, msg string)
	Playing func(idx int) // memoria in riproduzione, -1 nessuna
	Gap     time.Duration // pausa tra due memorie in coda (50 ms)
	Load    func(path string, c Config) (*PCM, error)

	queue chan int
	busy  atomic.Bool
	stop  atomic.Bool
}

func NewEngine(b Backend, st Store, cfg func() Config, status func(int, string), playing func(int)) *Engine {
	return &Engine{B: b, Store: st, Cfg: cfg, Status: status, Playing: playing,
		Gap: 50 * time.Millisecond, Load: LoadPrepared, queue: make(chan int, 64)}
}

func (e *Engine) Busy() bool { return e.busy.Load() }

// Enqueue mette in coda una memoria (0..7). Restituisce false se non accettata.
func (e *Engine) Enqueue(i int) bool {
	if i < 0 || i > 7 {
		return false
	}
	c := e.Cfg()
	if c.Memories[i] == "" {
		e.Status(StatusError, fmt.Sprintf("F%d: nessun file assegnato (usa il bottone File WAV...)", i+1))
		return false
	}
	select {
	case e.queue <- i:
		if e.busy.Load() {
			e.Status(StatusTX, fmt.Sprintf("TX in corso - in coda: %d", len(e.queue)))
		}
		return true
	default:
		return false
	}
}

func (e *Engine) drain() {
	for {
		select {
		case <-e.queue:
		default:
			return
		}
	}
}

// Stop svuota la coda, interrompe l'audio e rilascia il PTT.
func (e *Engine) Stop() {
	e.drain()
	if e.busy.Load() {
		e.stop.Store(true)
	}
}

// Run elabora la coda; va lanciato in una goroutine.
func (e *Engine) Run() {
	for idx := range e.queue {
		e.busy.Store(true)
		e.stop.Store(false)
		c := e.Cfg()
		pcm, err := e.Load(e.Store.Abs(c.Memories[idx]), c)
		if err != nil {
			e.drain()
			e.busy.Store(false)
			e.Status(StatusError, fmt.Sprintf("F%d: %v", idx+1, err))
			continue
		}
		tx, err := e.B.PTTOn(c)
		if err != nil {
			e.drain()
			e.busy.Store(false)
			e.Status(StatusError, "PTT: "+err.Error())
			continue
		}
		line := "RTS"
		where := c.Port
		switch {
		case c.UseTCI:
			line, where = "TCI", c.TCIAddr
		case c.UseDTR:
			line = "DTR"
		}
		e.Status(StatusTX, fmt.Sprintf("PTT %s %s attivo - attesa %d ms", where, line, c.DelayMs))
		deadline := time.Now().Add(time.Duration(ClampDelay(c.DelayMs)) * time.Millisecond)
		for time.Now().Before(deadline) && !e.stop.Load() {
			time.Sleep(time.Millisecond)
		}

		cur := idx
		lastErr := ""
	cycle:
		for {
			if e.stop.Load() {
				break
			}
			e.Playing(cur)
			e.Status(StatusTX, fmt.Sprintf("TX  F%d  %s   (%s %s)   in coda: %d", cur+1,
				filepath.Base(e.Store.Abs(c.Memories[cur])), where, line, len(e.queue)))
			if err := tx.Play(pcm, e.stop.Load); err != nil {
				lastErr = fmt.Sprintf("F%d: %v", cur+1, err)
			}
			// memoria successiva in coda: pausa di 50 ms, PTT sempre attivo
			pcm = nil
			for pcm == nil {
				if e.stop.Load() {
					break cycle
				}
				var next int
				select {
				case next = <-e.queue:
				default:
					break cycle
				}
				e.Playing(-1)
				e.Status(StatusTX, fmt.Sprintf("TX - pausa %d ms, poi F%d", e.Gap.Milliseconds(), next+1))
				t0 := time.Now()
				c2 := e.Cfg()
				p2, err := e.Load(e.Store.Abs(c2.Memories[next]), c2)
				if d := e.Gap - time.Since(t0); d > 0 {
					time.Sleep(d) // pausa tra due memorie (preparazione inclusa)
				}
				if err != nil {
					lastErr = fmt.Sprintf("F%d: %v", next+1, err)
					continue
				}
				c.Memories = c2.Memories
				pcm, cur = p2, next
			}
		}
		tx.Release()
		e.Playing(-1)
		stopped := e.stop.Load()
		if stopped {
			e.drain()
		}
		e.stop.Store(false)
		e.busy.Store(false)
		switch {
		case lastErr != "":
			e.Status(StatusError, lastErr)
		case stopped:
			e.Status(StatusNormal, "Trasmissione interrotta (Esc) - PTT rilasciato")
		default:
			e.Status(StatusNormal, "Pronto - PTT rilasciato")
		}
	}
}
