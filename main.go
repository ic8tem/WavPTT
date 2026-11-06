// WavPTT - riproduttore di messaggi WAV con PTT su porta seriale (RTS o DTR).
// Windows, Mac e Linux.
package main

import (
	_ "embed"
	"errors"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"
	"github.com/ncruces/zenity"

	"wavptt/core"
)

const appTitle = "WavPTT v12 - messaggi WAV con PTT"

//go:embed Icon.png
var iconPNG []byte

var (
	fyApp   fyne.App
	mainWin fyne.Window
	memWin  fyne.Window
	helpWin fyne.Window

	store core.Store
	cfgMu sync.Mutex
	cfg   core.Config

	hwb *hw
	eng *core.Engine

	memBtns  [8]*rbtn
	tciEd    *widget.Entry
	selTheme *widget.Select
	selMixer *widget.Select
	selCom   *widget.Select
	radio    *widget.RadioGroup
	delayEd  *delayEntry
	slider   *widget.Slider
	selFilt  *widget.Select
	selComp  *widget.Select
	statusLb *widget.Label
	warnLb   *widget.Label

	fireMu   sync.Mutex
	lastFire [8]time.Time

	releaseHotkeys = func() {}
	previewGen     atomic.Int64
)

func snapshot() core.Config {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	return cfg
}

func update(f func(c *core.Config)) {
	cfgMu.Lock()
	f(&cfg)
	c := cfg
	cfgMu.Unlock()
	if err := store.Save(c); err != nil {
		setStatus(core.StatusError, "Impossibile salvare WavPTT.ini nella cartella del programma: "+err.Error())
	}
}

// ---------------------------------------------------------------- stato

func setStatus(kind int, msg string) {
	if statusLb == nil {
		return
	}
	switch kind {
	case core.StatusTX:
		statusLb.Importance = widget.DangerImportance
	case core.StatusError:
		statusLb.Importance = widget.WarningImportance
	default:
		statusLb.Importance = widget.MediumImportance
	}
	statusLb.SetText(msg)
	statusLb.Refresh()
}

func setPlaying(idx int) {
	for i, b := range memBtns {
		b.SetTX(i == idx)
	}
}

// fire: memoria richiesta da bottone, tasto o tasto globale. Se la stessa
// memoria arriva due volte entro 200 ms (tasto locale + globale) conta una volta.
func fire(i int) {
	fireMu.Lock()
	if time.Since(lastFire[i]) < 200*time.Millisecond {
		fireMu.Unlock()
		return
	}
	lastFire[i] = time.Now()
	fireMu.Unlock()
	eng.Enqueue(i)
}

func handleKey(k *fyne.KeyEvent) bool {
	switch k.Name {
	case fyne.KeyF1, fyne.KeyF2, fyne.KeyF3, fyne.KeyF4, fyne.KeyF5, fyne.KeyF6, fyne.KeyF7, fyne.KeyF8:
		fire(int(k.Name[1] - '1'))
		return true
	case fyne.KeyEscape:
		eng.Stop()
		return true
	}
	return false
}

func unfocus() {
	if mainWin != nil {
		mainWin.Canvas().Unfocus()
	}
}

// ---------------------------------------------------------------- campo ritardo

// delayEntry: accetta solo cifre (0..300) e lascia passare F1..F8 ed Esc.
type delayEntry struct{ widget.Entry }

func newDelayEntry() *delayEntry {
	e := &delayEntry{}
	e.ExtendBaseWidget(e)
	return e
}

func (e *delayEntry) TypedRune(r rune) {
	if r >= '0' && r <= '9' {
		e.Entry.TypedRune(r)
	}
}

func (e *delayEntry) KeyDown(k *fyne.KeyEvent) {
	if handleKey(k) {
		return
	}
	e.Entry.KeyDown(k)
}

func (e *delayEntry) TypedKey(k *fyne.KeyEvent) {
	switch k.Name {
	case fyne.KeyF1, fyne.KeyF2, fyne.KeyF3, fyne.KeyF4, fyne.KeyF5, fyne.KeyF6, fyne.KeyF7, fyne.KeyF8, fyne.KeyEscape:
		return // già gestiti in KeyDown
	case fyne.KeyReturn, fyne.KeyEnter:
		unfocus()
		return
	}
	e.Entry.TypedKey(k)
}

func (e *delayEntry) FocusLost() {
	e.Entry.FocusLost()
	commitDelay()
}

func commitDelay() {
	v, err := strconv.Atoi(strings.TrimSpace(delayEd.Text))
	if err != nil {
		v = 0
	}
	v = core.ClampDelay(v)
	if delayEd.Text != strconv.Itoa(v) {
		delayEd.SetText(strconv.Itoa(v))
	}
	update(func(c *core.Config) { c.DelayMs = v })
}

// ---------------------------------------------------------------- elenchi

func sameList(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// refreshLists aggiorna gli elenchi di schede audio e porte (chiamata all'avvio
// e ogni 3 secondi, così un'interfaccia USB collegata dopo compare da sola).
func refreshLists() {
	c := snapshot()
	if devs := hwb.DeviceNames(); !sameList(devs, selMixer.Options) {
		selMixer.Options = devs
		selMixer.Refresh()
	}
	if ports := comPorts(); !sameList(ports, selCom.Options) {
		selCom.Options = ports
		selCom.Refresh()
	}
	showChoice(selMixer, c.TxMixer)
	showChoice(selCom, c.Port)
}

// showChoice mostra la scelta salvata; se il dispositivo non è collegato la
// scelta resta valida e viene ritrovata quando ricompare.
func showChoice(s *widget.Select, saved string) {
	if saved == "" {
		if s.PlaceHolder != "(scegli)" {
			s.PlaceHolder = "(scegli)"
			s.Refresh()
		}
		return
	}
	for _, o := range s.Options {
		if o == saved {
			if s.Selected != saved {
				s.SetSelected(saved)
			}
			return
		}
	}
	if s.Selected != "" {
		s.ClearSelected()
	}
	ph := saved + " (non presente)"
	if s.PlaceHolder != ph {
		s.PlaceHolder = ph
		s.Refresh()
	}
}

// ---------------------------------------------------------------- finestra principale

func fixed(w, h float32, o fyne.CanvasObject) fyne.CanvasObject {
	return container.NewGridWrap(fyne.NewSize(w, h), o)
}

func buildMain() fyne.CanvasObject {
	c := snapshot()

	grid := container.NewGridWithColumns(8)
	for i := 0; i < 8; i++ {
		i := i
		memBtns[i] = newRBtn(fmt.Sprintf("F%d", i+1), func() { fire(i) })
		grid.Add(memBtns[i])
	}
	row1 := container.NewBorder(nil, nil, nil,
		fixed(146, 42, newRBtn("File WAV...", openMemWin)), grid)

	selMixer = widget.NewSelect(nil, func(s string) {
		if s != "" && s != snapshot().TxMixer {
			update(func(c *core.Config) { c.TxMixer = s })
		}
		unfocus()
	})
	selMixer.PlaceHolder = "(scegli)"
	selCom = widget.NewSelect(nil, func(s string) {
		if s != "" && s != snapshot().Port {
			update(func(c *core.Config) { c.Port = s })
		}
		unfocus()
	})
	selCom.PlaceHolder = "(scegli)"
	// PTT tramite TCI: indirizzo del server al posto della porta seriale
	tciEd = widget.NewEntry()
	tciEd.SetText(c.TCIAddr)
	tciEd.SetPlaceHolder(core.DefaultTCIAddr)
	tciEd.OnChanged = func(s string) {
		update(func(c *core.Config) { c.TCIAddr = strings.TrimSpace(s) })
	}
	tciEd.OnSubmitted = func(string) { unfocus() }
	radio = widget.NewRadioGroup([]string{"RTS", "DTR", "TCI"}, func(s string) {
		if s == "" {
			return
		}
		dtr, tci := s == "DTR", s == "TCI"
		cur := snapshot()
		if dtr != cur.UseDTR || tci != cur.UseTCI {
			update(func(c *core.Config) { c.UseDTR, c.UseTCI = dtr, tci })
		}
		showPTTMode(tci)
		unfocus()
	})
	radio.Horizontal = true
	radio.Required = true
	switch {
	case c.UseTCI:
		radio.SetSelected("TCI")
	case c.UseDTR:
		radio.SetSelected("DTR")
	default:
		radio.SetSelected("RTS")
	}
	delayEd = newDelayEntry()
	delayEd.SetText(strconv.Itoa(c.DelayMs))
	delayEd.OnChanged = func(s string) {
		digits := strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, s)
		if digits != s {
			delayEd.SetText(digits)
			return
		}
		if v, err := strconv.Atoi(digits); err == nil && v > 300 {
			delayEd.SetText("300")
		}
	}
	row2 := container.NewHBox(
		widget.NewLabel("TX MIXER"), fixed(230, 38, selMixer),
		widget.NewLabel("PTT"), fixed(160, 38, container.NewStack(selCom, tciEd)),
		radio,
		widget.NewLabel("Ritardo (ms)"), fixed(70, 38, delayEd),
	)

	// volume: cursore 0..100 con andamento logaritmico, a ogni avvio al massimo
	slider = widget.NewSlider(0, 100)
	slider.Step = 1
	slider.SetValue(100)
	slider.OnChangeEnded = func(v float64) {
		update(func(c *core.Config) { c.Volume = int(v) })
		unfocus()
	}
	selFilt = widget.NewSelect(core.FilterNames, func(s string) {
		for k, n := range core.FilterNames {
			if n == s && k != snapshot().FilterMode {
				update(func(c *core.Config) { c.FilterMode = k })
			}
		}
		unfocus()
	})
	selFilt.SetSelectedIndex(c.FilterMode)
	compNames := []string{"Compressore off"}
	for db := 3; db <= 13; db++ {
		compNames = append(compNames, fmt.Sprintf("Compressore %d dB", db))
	}
	selComp = widget.NewSelect(compNames, func(s string) {
		db := 0
		fmt.Sscanf(s, "Compressore %d dB", &db)
		if db != snapshot().CompDB {
			update(func(c *core.Config) { c.CompDB = db })
		}
		unfocus()
	})
	if c.CompDB >= 3 {
		selComp.SetSelectedIndex(c.CompDB - 2)
	} else {
		selComp.SetSelectedIndex(0)
	}
	row3 := container.NewBorder(nil, nil, widget.NewLabel("VOLUME"),
		container.NewHBox(fixed(150, 38, selFilt), fixed(190, 38, selComp)), slider)

	warnLb = widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	warnLb.Importance = widget.DangerImportance
	warnLb.Truncation = fyne.TextTruncateEllipsis
	warnLb.Hide()

	statusLb = widget.NewLabel("")
	statusLb.Truncation = fyne.TextTruncateEllipsis
	selTheme = widget.NewSelect(ThemeNames, func(s string) {
		if s != "" && s != snapshot().Theme {
			update(func(c *core.Config) { c.Theme = s })
			applyTheme(s)
		}
		unfocus()
	})
	selTheme.SetSelected(themeName(c.Theme))
	right := container.NewHBox(widget.NewLabel("Colori"), fixed(120, 38, selTheme), fixed(80, 38, newRBtn("Help", openHelp)))
	row5 := container.NewBorder(nil, nil, nil, right, statusLb)

	showPTTMode(c.UseTCI)
	return container.NewPadded(container.NewVBox(row1, row2, row3, warnLb, row5))
}

// themeName: schema salvato, oppure Notte se sconosciuto.
func themeName(s string) string {
	for _, n := range ThemeNames {
		if n == s {
			return n
		}
	}
	return "Notte"
}

// showPTTMode: con TCI si inserisce l'indirizzo del server al posto della
// porta seriale, e TX MIXER non serve (l'audio va alla radio tramite TCI).
func showPTTMode(tci bool) {
	if tci {
		selCom.Hide()
		tciEd.Show()
		selMixer.Disable()
	} else {
		tciEd.Hide()
		selCom.Show()
		selMixer.Enable()
	}
}

func showHotkeyWarning(text string) {
	warnLb.SetText(text)
	warnLb.Show()
	mainWin.Resize(fyne.NewSize(mainWin.Canvas().Size().Width, mainWin.Content().MinSize().Height))
	go func() {
		// lampeggio per 3 secondi (testo acceso/spento ogni 250 ms), poi resta fisso
		for i := 0; i < 12; i++ {
			time.Sleep(250 * time.Millisecond)
			if i%2 == 0 {
				warnLb.SetText(" ")
			} else {
				warnLb.SetText(text)
			}
		}
		warnLb.SetText(text)
	}()
}

// ---------------------------------------------------------------- finestra memorie

func openMemWin() {
	unfocus()
	if memWin != nil {
		memWin.RequestFocus()
		return
	}
	c := snapshot()
	var tmp [8]string
	for i := range tmp {
		tmp[i] = store.Abs(c.Memories[i])
	}
	w := fyApp.NewWindow("Memorie WAV (F1 - F8)")
	memWin = w
	hint := widget.NewLabel("Play: ascolto sull'uscita audio predefinita, senza PTT.")
	hint.Truncation = fyne.TextTruncateEllipsis

	var names [8]*widget.Entry
	rows := container.NewVBox()
	lastDir := ""
	for i := 0; i < 8; i++ {
		i := i
		lbl := canvas.NewText(fmt.Sprintf("F%d", i+1), color.NRGBA{R: 0x20, G: 0x40, B: 0xC0, A: 0xFF})
		lbl.TextStyle = fyne.TextStyle{Bold: true}
		lbl.TextSize = 16
		names[i] = widget.NewEntry()
		names[i].SetText(filepath.Base(tmp[i]))
		if tmp[i] == "" {
			names[i].SetText("")
		}
		names[i].Disable()
		setFile := func(p string) {
			if _, err := core.LoadWav(p); err != nil {
				dialog.ShowError(err, w)
				return
			}
			tmp[i] = p
			lastDir = filepath.Dir(p)
			names[i].SetText(filepath.Base(p))
		}
		browse := newRBtn("...", func() {
			start := lastDir
			if start == "" && tmp[i] != "" {
				start = filepath.Dir(tmp[i])
			}
			if start == "" {
				if st, err := os.Stat(store.WavDir()); err == nil && st.IsDir() {
					start = store.WavDir()
				}
			}
			go chooseWav(w, i, start, setFile)
		})
		play := newRBtn("Play", func() { startPreview(i, tmp[i], hint) })
		stop := newRBtn("Stop", func() { previewGen.Add(1) })
		rows.Add(container.NewBorder(nil, nil,
			container.NewGridWrap(fyne.NewSize(30, 36), container.NewCenter(lbl)),
			container.NewHBox(browse, play, stop), names[i]))
	}
	closeWin := func() {
		previewGen.Add(1)
		memWin = nil
		w.Close()
	}
	save := newRBtn("Salva", func() {
		stored, err := store.Import(tmp)
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		update(func(c *core.Config) { c.Memories = stored })
		closeWin()
		setStatus(core.StatusNormal, "Memorie salvate (file copiati nella cartella WAV)")
	})
	save.minW = 90
	cancel := newRBtn("Annulla", closeWin)
	cancel.minW = 90
	bottom := container.NewBorder(nil, nil, nil, container.NewHBox(save, cancel), hint)
	w.SetContent(container.NewPadded(container.NewBorder(nil, bottom, nil, nil, rows)))
	w.SetCloseIntercept(closeWin)
	w.Canvas().SetOnTypedKey(func(k *fyne.KeyEvent) {
		if k.Name == fyne.KeyEscape {
			closeWin()
		}
	})
	w.Resize(fyne.NewSize(620, 0))
	w.CenterOnScreen()
	w.Show()
}

// chooseWav apre la finestra di scelta del file del sistema operativo; se non è
// disponibile (per esempio Linux senza zenity) usa quella interna.
func chooseWav(parent fyne.Window, i int, start string, set func(string)) {
	opts := []zenity.Option{
		zenity.Title(fmt.Sprintf("Scegli il file WAV per F%d", i+1)),
		zenity.FileFilters{{Name: "File WAV", Patterns: []string{"*.wav"}, CaseFold: true}},
	}
	if start != "" {
		opts = append(opts, zenity.Filename(start+string(os.PathSeparator)))
	}
	p, err := zenity.SelectFile(opts...)
	switch {
	case err == nil:
		set(p)
		return
	case errors.Is(err, zenity.ErrCanceled):
		return
	}
	d := dialog.NewFileOpen(func(r fyne.URIReadCloser, err error) {
		if err != nil || r == nil {
			return
		}
		path := r.URI().Path()
		r.Close()
		set(path)
	}, parent)
	d.SetFilter(storage.NewExtensionFileFilter([]string{".wav", ".WAV", ".Wav"}))
	if start != "" {
		if l, err := storage.ListerForURI(storage.NewFileURI(start)); err == nil {
			d.SetLocation(l)
		}
	}
	parent.Resize(fyne.NewSize(860, 620))
	d.Resize(fyne.NewSize(840, 600))
	d.Show()
}

func startPreview(i int, path string, hint *widget.Label) {
	if path == "" {
		hint.SetText(fmt.Sprintf("F%d: nessun file", i+1))
		return
	}
	gen := previewGen.Add(1)
	go func() {
		pcm, err := core.LoadPrepared(path, snapshot())
		if err != nil {
			hint.SetText(err.Error())
			return
		}
		time.Sleep(40 * time.Millisecond) // lascia terminare un ascolto precedente
		if previewGen.Load() != gen {
			return
		}
		hint.SetText(fmt.Sprintf("Ascolto F%d: %s", i+1, filepath.Base(path)))
		err = hwb.Play(pcm, "", func() bool { return previewGen.Load() != gen })
		switch {
		case err != nil:
			hint.SetText(err.Error())
		case previewGen.Load() == gen:
			hint.SetText("Play: ascolto sull'uscita audio predefinita, senza PTT.")
		}
	}()
}

// ---------------------------------------------------------------- help

func openHelp() {
	unfocus()
	if helpWin != nil {
		helpWin.RequestFocus()
		return
	}
	w := fyApp.NewWindow("Help - WavPTT")
	helpWin = w
	txt := widget.NewLabelWithStyle(helpText, fyne.TextAlignLeading, fyne.TextStyle{Monospace: true})
	closeWin := func() {
		helpWin = nil
		w.Close()
	}
	btn := newRBtn("Chiudi", closeWin)
	btn.minW = 100
	w.SetContent(container.NewBorder(nil,
		container.NewPadded(container.NewHBox(layout.NewSpacer(), btn)), nil, nil,
		container.NewVScroll(txt)))
	w.SetCloseIntercept(closeWin)
	w.Resize(fyne.NewSize(720, 600))
	w.CenterOnScreen()
	w.Show()
}

// ---------------------------------------------------------------- main

func main() {
	store = core.Store{Dir: dataDir()}
	cfg = store.Load()
	cfg.Volume = 100 // il cursore del volume parte sempre dal massimo

	fyApp = app.NewWithID("it.ic8tem.wavptt")
	fyApp.SetIcon(fyne.NewStaticResource("Icon.png", iconPNG))
	applyTheme(themeName(cfg.Theme))
	mainWin = fyApp.NewWindow(appTitle)
	mainWin.SetMaster()

	// senza sistema audio funziona ancora il TCI (l'audio va alla radio in rete)
	var hwErr error
	hwb, hwErr = newHW()
	if hwErr != nil {
		hwb = &hw{}
	}
	var backend core.Backend = hwb

	mainWin.SetContent(buildMain())
	eng = core.NewEngine(backend, store, snapshot, setStatus, setPlaying)
	go eng.Run()
	refreshLists()

	if dc, ok := mainWin.Canvas().(desktop.Canvas); ok {
		dc.SetOnKeyDown(func(k *fyne.KeyEvent) { handleKey(k) })
	}

	if hwErr != nil {
		setStatus(core.StatusError, hwErr.Error())
	} else {
		setStatus(core.StatusNormal, "Pronto - F1..F8 ("+hotkeyLabel+"+F1..F8 da altri programmi), Esc interrompe")
	}

	fyApp.Lifecycle().SetOnStarted(func() {
		go func() {
			rel, warn := registerGlobalHotkeys(fire)
			releaseHotkeys = rel
			if warn != "" {
				showHotkeyWarning(warn)
				if hwErr == nil {
					setStatus(core.StatusNormal, "Pronto - F1..F8 con la finestra attiva, Esc interrompe")
				}
			}
		}()
		go func() {
			for range time.Tick(3 * time.Second) {
				refreshLists()
			}
		}()
	})

	mainWin.SetCloseIntercept(func() {
		commitDelay()
		eng.Stop()
		previewGen.Add(1)
		for i := 0; i < 100 && eng.Busy(); i++ {
			time.Sleep(10 * time.Millisecond)
		}
		releaseHotkeys()
		hwb.close()
		fyApp.Quit()
	})

	mainWin.Resize(fyne.NewSize(900, 0))
	mainWin.SetFixedSize(true)
	mainWin.CenterOnScreen()
	mainWin.ShowAndRun()
}
