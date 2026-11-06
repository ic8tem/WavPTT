package main

import (
	"image/color"
	"math"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// ---------------------------------------------------------------- schemi di colore

// palette: i colori di uno schema (gli stessi dei bozzetti).
type palette struct {
	bg, tbar, tx, b1, b2, bd, hi, lo, sh, fd, ac, t1, t2, tb, st, warn color.NRGBA
	dark                                                               bool
}

func hex(s string) color.NRGBA {
	var c color.NRGBA
	c.A = 0xFF
	v := func(i int) uint8 {
		h := func(b byte) uint8 {
			switch {
			case b >= '0' && b <= '9':
				return b - '0'
			case b >= 'a' && b <= 'f':
				return b - 'a' + 10
			case b >= 'A' && b <= 'F':
				return b - 'A' + 10
			}
			return 0
		}
		return h(s[i])<<4 | h(s[i+1])
	}
	c.R, c.G, c.B = v(1), v(3), v(5)
	return c
}

func rgba(r, g, b uint8, a float64) color.NRGBA {
	return color.NRGBA{R: r, G: g, B: b, A: uint8(math.Round(a * 255))}
}

// ThemeNames: voci del menu Colori.
var ThemeNames = []string{"Chiaro", "Notte", "Nero", "Blu", "Viola", "Verde"}

var palettes = map[string]palette{
	"Chiaro": {bg: hex("#F3F5F8"), tbar: hex("#FFFFFF"), tx: hex("#1B2330"), b1: hex("#FFFFFF"), b2: hex("#E1E7EF"), bd: hex("#A9B4C2"),
		hi: rgba(255, 255, 255, .95), lo: rgba(30, 50, 80, .14), sh: rgba(20, 35, 60, .22), fd: hex("#FFFFFF"), ac: hex("#1F6FEB"),
		t1: hex("#E0453A"), t2: hex("#B32218"), tb: hex("#8E1A12"), st: hex("#B32218"), warn: hex("#9A4D00")},
	"Notte": {bg: hex("#0F1A2B"), tbar: hex("#0A1321"), tx: hex("#DCE6F5"), b1: hex("#2A4268"), b2: hex("#1A2C48"), bd: hex("#3F5F8F"),
		hi: rgba(160, 200, 255, .30), lo: rgba(0, 0, 0, .35), sh: rgba(0, 0, 0, .55), fd: hex("#0B1526"), ac: hex("#5AA2FF"),
		t1: hex("#E04A40"), t2: hex("#A82A20"), tb: hex("#E57A70"), st: hex("#FF8A80"), warn: hex("#FFB86B"), dark: true},
	"Nero": {bg: hex("#0B0B0B"), tbar: hex("#000000"), tx: hex("#EDEDED"), b1: hex("#2E2E2E"), b2: hex("#171717"), bd: hex("#4A4A4A"),
		hi: rgba(255, 255, 255, .18), lo: rgba(0, 0, 0, .6), sh: rgba(0, 0, 0, .8), fd: hex("#050505"), ac: hex("#FF9F1C"),
		t1: hex("#E04A40"), t2: hex("#A82A20"), tb: hex("#E57A70"), st: hex("#FF8A80"), warn: hex("#FFB86B"), dark: true},
	"Blu": {bg: hex("#12407A"), tbar: hex("#0D3366"), tx: hex("#FFFFFF"), b1: hex("#3878C8"), b2: hex("#1F5296"), bd: hex("#7FAEE6"),
		hi: rgba(255, 255, 255, .40), lo: rgba(0, 20, 60, .35), sh: rgba(0, 15, 45, .55), fd: hex("#0C3162"), ac: hex("#FFD24A"),
		t1: hex("#E8564A"), t2: hex("#B02E22"), tb: hex("#FFB0A8"), st: hex("#FFD24A"), warn: hex("#FFD24A"), dark: true},
	"Viola": {bg: hex("#2A1A40"), tbar: hex("#1F1232"), tx: hex("#F1E9FB"), b1: hex("#6A45A0"), b2: hex("#472C70"), bd: hex("#9C7ACC"),
		hi: rgba(230, 200, 255, .35), lo: rgba(20, 0, 40, .4), sh: rgba(10, 0, 25, .6), fd: hex("#1E1131"), ac: hex("#E59CFF"),
		t1: hex("#E04A60"), t2: hex("#A82A40"), tb: hex("#F59AAE"), st: hex("#FFA3C2"), warn: hex("#FFC27A"), dark: true},
	"Verde": {bg: hex("#12301F"), tbar: hex("#0C2416"), tx: hex("#E6F6EC"), b1: hex("#357A52"), b2: hex("#215237"), bd: hex("#66AD85"),
		hi: rgba(200, 255, 220, .30), lo: rgba(0, 25, 10, .4), sh: rgba(0, 15, 5, .6), fd: hex("#0B2215"), ac: hex("#A6E35F"),
		t1: hex("#E0563E"), t2: hex("#A8331F"), tb: hex("#F5A08E"), st: hex("#FFB59E"), warn: hex("#FFD27A"), dark: true},
}

var (
	palMu sync.RWMutex
	pal   = palettes["Notte"]
)

func curPal() palette {
	palMu.RLock()
	defer palMu.RUnlock()
	return pal
}

// applyTheme imposta lo schema e ridisegna tutto.
func applyTheme(name string) {
	p, ok := palettes[name]
	if !ok {
		p = palettes["Notte"]
	}
	palMu.Lock()
	pal = p
	palMu.Unlock()
	if fyApp != nil {
		fyApp.Settings().SetTheme(wavTheme{})
	}
	for _, b := range allButtons {
		b.Refresh()
	}
}

func alpha(c color.NRGBA, a uint8) color.NRGBA { c.A = a; return c }

// wavTheme: tema Fyne con i colori dello schema scelto e angoli smussati.
type wavTheme struct{}

func (wavTheme) Color(n fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	p := curPal()
	v := theme.VariantLight
	if p.dark {
		v = theme.VariantDark
	}
	switch n {
	case theme.ColorNameBackground:
		return p.bg
	case theme.ColorNameForeground:
		return p.tx
	case theme.ColorNameButton:
		return p.b2
	case theme.ColorNameDisabledButton:
		return alpha(p.b2, 0x99)
	case theme.ColorNameDisabled:
		return alpha(p.tx, 0x70)
	case theme.ColorNameInputBackground:
		return p.fd
	case theme.ColorNameInputBorder:
		return p.bd
	case theme.ColorNamePrimary, theme.ColorNameHyperlink:
		return p.ac
	case theme.ColorNameFocus:
		return alpha(p.ac, 0x70)
	case theme.ColorNameSelection:
		return alpha(p.ac, 0x50)
	case theme.ColorNameHover:
		return alpha(p.tx, 0x1C)
	case theme.ColorNamePressed:
		return alpha(p.tx, 0x30)
	case theme.ColorNamePlaceHolder:
		return alpha(p.tx, 0xA0)
	case theme.ColorNameScrollBar:
		return alpha(p.tx, 0x60)
	case theme.ColorNameSeparator:
		return p.bd
	case theme.ColorNameShadow:
		return p.sh
	case theme.ColorNameMenuBackground, theme.ColorNameOverlayBackground, theme.ColorNameHeaderBackground:
		return p.tbar
	case theme.ColorNameError:
		return p.st
	case theme.ColorNameWarning:
		return p.warn
	case theme.ColorNameForegroundOnPrimary:
		if p.dark {
			return color.NRGBA{A: 0xFF}
		}
		return color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
	}
	return theme.DefaultTheme().Color(n, v)
}

func (wavTheme) Font(s fyne.TextStyle) fyne.Resource     { return theme.DefaultTheme().Font(s) }
func (wavTheme) Icon(n fyne.ThemeIconName) fyne.Resource { return theme.DefaultTheme().Icon(n) }

func (wavTheme) Size(n fyne.ThemeSizeName) float32 {
	switch n {
	case theme.SizeNameInputRadius:
		return btnRadius
	case theme.SizeNameSelectionRadius:
		return 6
	}
	return theme.DefaultTheme().Size(n)
}

// ---------------------------------------------------------------- pulsante in rilievo

const btnRadius = 8 // angoli smussati (px)

var allButtons []*rbtn

// rbtn: pulsante con angoli smussati e bordo in rilievo (luce in alto, ombra in basso).
type rbtn struct {
	widget.BaseWidget
	Text     string
	OnTapped func()
	TX       bool // in trasmissione: rosso
	hovered  bool
	pressed  bool
	minW     float32
}

func newRBtn(text string, tap func()) *rbtn {
	b := &rbtn{Text: text, OnTapped: tap}
	b.ExtendBaseWidget(b)
	allButtons = append(allButtons, b)
	return b
}

func (b *rbtn) SetText(t string) { b.Text = t; b.Refresh() }

func (b *rbtn) SetTX(on bool) {
	if b.TX != on {
		b.TX = on
		b.Refresh()
	}
}

func (b *rbtn) Tapped(*fyne.PointEvent) {
	if b.OnTapped != nil {
		b.OnTapped()
	}
}

func (b *rbtn) Cursor() desktop.Cursor         { return desktop.PointerCursor }
func (b *rbtn) MouseIn(*desktop.MouseEvent)    { b.hovered = true; b.Refresh() }
func (b *rbtn) MouseMoved(*desktop.MouseEvent) {}
func (b *rbtn) MouseOut()                      { b.hovered, b.pressed = false, false; b.Refresh() }
func (b *rbtn) MouseDown(*desktop.MouseEvent)  { b.pressed = true; b.Refresh() }
func (b *rbtn) MouseUp(*desktop.MouseEvent)    { b.pressed = false; b.Refresh() }

func (b *rbtn) CreateRenderer() fyne.WidgetRenderer {
	r := &rbtnRenderer{b: b}
	r.bg = canvas.NewRasterWithPixels(r.pixel)
	r.label = canvas.NewText(b.Text, curPal().tx)
	r.label.TextStyle = fyne.TextStyle{Bold: true}
	r.label.TextSize = theme.TextSize() + 1
	r.label.Alignment = fyne.TextAlignCenter
	return r
}

type rbtnRenderer struct {
	b     *rbtn
	bg    *canvas.Raster
	label *canvas.Text
}

const shadowPx = 2 // ombra sotto il pulsante (px)

func (r *rbtnRenderer) Layout(s fyne.Size) {
	r.bg.Resize(s)
	r.bg.Move(fyne.NewPos(0, 0))
	ts := r.label.MinSize()
	dy := float32(0)
	if r.b.pressed {
		dy = 1
	}
	r.label.Resize(fyne.NewSize(s.Width, ts.Height))
	r.label.Move(fyne.NewPos(0, (s.Height-shadowPx-ts.Height)/2+dy))
}

func (r *rbtnRenderer) MinSize() fyne.Size {
	ts := r.label.MinSize()
	w := ts.Width + 28
	if w < r.b.minW {
		w = r.b.minW
	}
	return fyne.NewSize(w, fyne.Max(ts.Height+16, 38))
}

func (r *rbtnRenderer) Refresh() {
	p := curPal()
	r.label.Text = r.b.Text
	if r.b.TX {
		r.label.Color = color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
	} else {
		r.label.Color = p.tx
	}
	r.label.TextSize = theme.TextSize() + 1
	r.label.Refresh()
	r.Layout(r.b.Size())
	r.bg.Refresh()
}

func (r *rbtnRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.bg, r.label} }
func (r *rbtnRenderer) Destroy()                     {}

func mix(a, b color.NRGBA, t float64) color.NRGBA {
	l := func(x, y uint8) uint8 { return uint8(math.Round(float64(x) + (float64(y)-float64(x))*t)) }
	return color.NRGBA{R: l(a.R, b.R), G: l(a.G, b.G), B: l(a.B, b.B), A: l(a.A, b.A)}
}

// over: colore c (con trasparenza) sopra il fondo dst.
func over(dst, c color.NRGBA) color.NRGBA {
	a := float64(c.A) / 255
	out := mix(dst, color.NRGBA{R: c.R, G: c.G, B: c.B, A: 0xFF}, a)
	out.A = dst.A
	if c.A > dst.A {
		out.A = uint8(math.Round(float64(dst.A) + (255-float64(dst.A))*a))
	}
	return out
}

// pixel disegna il pulsante: ombra, corpo sfumato, bordo, luce in alto e ombra interna in basso.
func (r *rbtnRenderer) pixel(x, y, w, h int) color.Color {
	b := r.b
	p := curPal()
	logical := b.Size().Height
	sc := float64(1)
	if logical > 0 {
		sc = float64(h) / float64(logical)
	}
	shadow := shadowPx * sc
	rad := btnRadius * sc
	bodyH := float64(h) - shadow
	fx, fy := float64(x)+0.5, float64(y)+0.5

	// distanza dal bordo di un rettangolo smussato (negativa all'interno)
	sdf := func(px, py, top, height float64) float64 {
		cx, cy := float64(w)/2, top+height/2
		qx := math.Abs(px-cx) - (float64(w)/2 - rad)
		qy := math.Abs(py-cy) - (height/2 - rad)
		ox, oy := math.Max(qx, 0), math.Max(qy, 0)
		return math.Hypot(ox, oy) + math.Min(math.Max(qx, qy), 0) - rad
	}

	var out color.NRGBA
	// ombra esterna, spostata in basso
	if ds := sdf(fx, fy, shadow, bodyH); ds < 1 {
		cov := math.Min(1, math.Max(0, 0.5-ds))
		out = alpha(p.sh, uint8(float64(p.sh.A)*cov))
	}
	d := sdf(fx, fy, 0, bodyH)
	if d >= 0.5 {
		return out
	}
	cov := math.Min(1, math.Max(0, 0.5-d))

	top, bot, border := p.b1, p.b2, p.bd
	if b.TX {
		top, bot, border = p.t1, p.t2, p.tb
	}
	t := fy / bodyH
	if b.pressed {
		t = 1 - t // incassato mentre è premuto
	}
	c := mix(top, bot, math.Min(1, math.Max(0, t)))
	if b.hovered && !b.pressed {
		c = mix(c, top, 0.35)
	}
	edge := -d // distanza dal bordo verso l'interno
	switch {
	case edge < 1*sc:
		c = border
	case !b.pressed && fy < 1*sc+1.5*sc:
		c = over(c, p.hi) // luce sul bordo alto
	case !b.pressed && fy > bodyH-1*sc-2*sc:
		c = over(c, p.lo) // ombra interna sul bordo basso
	case b.pressed && fy < 1*sc+2*sc:
		c = over(c, p.lo)
	}
	c.A = 0xFF
	if cov >= 1 {
		return c
	}
	if out.A == 0 {
		return alpha(c, uint8(255*cov))
	}
	return mix(out, c, cov)
}
