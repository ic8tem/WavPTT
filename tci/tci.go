// Package tci: client minimo del protocollo TCI (Expert Electronics SunSDR /
// ExpertSDR e programmi compatibili) per PTT e audio di trasmissione.
//
// Il TCI è un WebSocket: comandi testuali "nome:arg1,arg2;" e flussi audio in
// frame binari con intestazione di 16 valori uint32 (64 byte) seguita dai campioni:
// ricevitore, frequenza di campionamento, formato, codec, crc, lunghezza,
// tipo (0 IQ, 1 audio RX, 2 audio TX, 3 tx_chrono), canali, riservati.
// In trasmissione con sorgente "tci" la radio manda frame tx_chrono che
// chiedono "lunghezza" campioni; il programma risponde con un frame audio TX
// della stessa lunghezza (campioni interlacciati per canale).
package tci

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	typeTXAudio  = 2
	typeTXChrono = 3
	headerLen    = 64
)

// Conn: collegamento a un server TCI.
type Conn struct {
	ws   *websocket.Conn
	wmu  sync.Mutex // una scrittura alla volta sul WebSocket
	done chan struct{}

	mu       sync.Mutex
	err      error
	rate     int // frequenza chiesta dalla radio (dai tx_chrono)
	rateSeen chan struct{}
	buf      []float32 // audio mono da trasmettere, alla frequenza della radio
	pos      int
	silence  int // campioni di silenzio mandati dopo la fine dell'audio
	drain    int // silenzio da mandare prima di dichiarare finito l'audio
	finished chan struct{}
	ptt      bool
}

// Dial si collega al server TCI (host:porta), attende "ready;" e imposta l'audio a rate Hz.
func Dial(addr string, rate int) (*Conn, error) {
	d := websocket.Dialer{HandshakeTimeout: 3 * time.Second}
	ws, _, err := d.Dial("ws://"+addr, nil)
	if err != nil {
		return nil, fmt.Errorf("TCI: impossibile collegarsi a %s (ExpertSDR acceso e TCI abilitato?)", addr)
	}
	c := &Conn{ws: ws, done: make(chan struct{}), rateSeen: make(chan struct{})}
	ready := make(chan struct{})
	go c.readLoop(ready)
	select {
	case <-ready:
	case <-c.done:
		return nil, c.lastErr()
	case <-time.After(3 * time.Second):
		// alcuni server non mandano "ready;": si prosegue lo stesso
	}
	if err := c.Send(fmt.Sprintf("audio_samplerate:%d;", rate)); err != nil {
		c.Close()
		return nil, err
	}
	if err := c.Send("audio_start:0;"); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

func (c *Conn) lastErr() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return c.err
	}
	return errors.New("TCI: collegamento chiuso")
}

// Send manda un comando testuale.
func (c *Conn) Send(cmd string) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_ = c.ws.SetWriteDeadline(time.Now().Add(2 * time.Second))
	if err := c.ws.WriteMessage(websocket.TextMessage, []byte(cmd)); err != nil {
		return fmt.Errorf("TCI: invio non riuscito: %v", err)
	}
	return nil
}

// PTT attiva (sorgente audio TCI) o rilascia la trasmissione sul ricevitore 0.
func (c *Conn) PTT(on bool) error {
	c.mu.Lock()
	c.ptt = on
	c.mu.Unlock()
	if on {
		return c.Send("trx:0,true,tci;")
	}
	return c.Send("trx:0,false;")
}

// Close rilascia il PTT se attivo e chiude il collegamento.
func (c *Conn) Close() {
	c.mu.Lock()
	ptt := c.ptt
	c.mu.Unlock()
	if ptt {
		_ = c.PTT(false)
	}
	_ = c.Send("audio_stop:0;")
	c.wmu.Lock()
	_ = c.ws.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	c.wmu.Unlock()
	_ = c.ws.Close()
}

// WaitRate attende il primo tx_chrono e restituisce la frequenza chiesta dalla radio.
func (c *Conn) WaitRate(timeout time.Duration) (int, error) {
	select {
	case <-c.rateSeen:
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.rate, nil
	case <-c.done:
		return 0, c.lastErr()
	case <-time.After(timeout):
		return 0, errors.New("TCI: la radio non chiede l'audio (in ExpertSDR il TCI deve poter trasmettere)")
	}
}

// Play trasmette i campioni mono (float -1..1, alla frequenza di WaitRate) e
// ritorna quando sono stati consegnati alla radio, più un margine di silenzio
// perché la coda della frase non venga tagliata al rilascio del PTT.
func (c *Conn) Play(samples []float32, stop func() bool) error {
	c.mu.Lock()
	rate := c.rate
	if rate <= 0 {
		rate = 8000
	}
	c.buf, c.pos, c.silence = samples, 0, 0
	c.drain = rate * 150 / 1000
	fin := make(chan struct{})
	c.finished = fin
	c.mu.Unlock()
	dur := time.Duration(len(samples)) * time.Second / time.Duration(rate)
	limit := time.After(dur + 5*time.Second)
	for {
		select {
		case <-fin:
			return nil
		case <-c.done:
			return c.lastErr()
		case <-limit:
			c.clearAudio()
			return errors.New("TCI: la radio ha smesso di chiedere l'audio")
		case <-time.After(5 * time.Millisecond):
			if stop() {
				c.clearAudio()
				return nil
			}
		}
	}
}

func (c *Conn) clearAudio() {
	c.mu.Lock()
	c.buf, c.pos, c.finished = nil, 0, nil
	c.mu.Unlock()
}

func (c *Conn) readLoop(ready chan struct{}) {
	defer close(c.done)
	readyOnce := sync.Once{}
	for {
		mt, data, err := c.ws.ReadMessage()
		if err != nil {
			c.mu.Lock()
			if c.err == nil {
				c.err = errors.New("TCI: collegamento interrotto")
			}
			c.mu.Unlock()
			return
		}
		switch mt {
		case websocket.TextMessage:
			for _, cmd := range strings.Split(string(data), ";") {
				if strings.EqualFold(strings.TrimSpace(cmd), "ready") {
					readyOnce.Do(func() { close(ready) })
				}
			}
		case websocket.BinaryMessage:
			if len(data) >= headerLen && binary.LittleEndian.Uint32(data[24:28]) == typeTXChrono {
				c.answerChrono(data[:headerLen])
			}
		}
	}
}

// answerChrono risponde a una richiesta di campioni con il frame audio TX.
func (c *Conn) answerChrono(h []byte) {
	rate := int(binary.LittleEndian.Uint32(h[4:8]))
	format := binary.LittleEndian.Uint32(h[8:12])
	n := int(binary.LittleEndian.Uint32(h[20:24]))
	ch := int(binary.LittleEndian.Uint32(h[28:32]))
	if ch < 1 || ch > 2 {
		ch = 2 // predefinito TCI: stereo
	}
	if n <= 0 || n > 1<<20 {
		return
	}
	bps := 4 // float32 (predefinito)
	switch format {
	case 0:
		bps = 2 // int16
	case 1:
		bps = 3 // int24
	}

	c.mu.Lock()
	if c.rate == 0 && rate > 0 {
		c.rate = rate
		close(c.rateSeen)
	}
	out := make([]byte, headerLen+n*bps)
	copy(out, h)
	binary.LittleEndian.PutUint32(out[24:28], typeTXAudio)
	frames := n / ch
	for f := 0; f < frames; f++ {
		var v float32
		if c.pos < len(c.buf) {
			v = c.buf[c.pos]
			c.pos++
		} else if c.finished != nil {
			c.silence++
		}
		for k := 0; k < ch; k++ {
			putSample(out[headerLen+(f*ch+k)*bps:], v, format)
		}
	}
	if c.finished != nil && c.pos >= len(c.buf) && c.silence >= c.drain {
		close(c.finished)
		c.finished = nil
	}
	c.mu.Unlock()

	c.wmu.Lock()
	_ = c.ws.SetWriteDeadline(time.Now().Add(time.Second))
	_ = c.ws.WriteMessage(websocket.BinaryMessage, out)
	c.wmu.Unlock()
}

func putSample(b []byte, v float32, format uint32) {
	if v > 1 {
		v = 1
	} else if v < -1 {
		v = -1
	}
	switch format {
	case 0:
		binary.LittleEndian.PutUint16(b, uint16(int16(math.Round(float64(v)*32767))))
	case 1:
		x := int32(math.Round(float64(v) * 8388607))
		b[0], b[1], b[2] = byte(x), byte(x>>8), byte(x>>16)
	case 2:
		binary.LittleEndian.PutUint32(b, uint32(int32(math.Round(float64(v)*2147483647))))
	default:
		binary.LittleEndian.PutUint32(b, math.Float32bits(v))
	}
}
