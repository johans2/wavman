package main

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"log"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gioui.org/app"
	"gioui.org/font"
	"gioui.org/font/gofont"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"github.com/ncruces/zenity"
)

const sampleRate = 44100

type sliderState struct {
	f      widget.Float
	min    float64
	max    float64
	label  string
	format func(float64) string
}

func newSlider(label string, min, max, init float64, format func(float64) string) *sliderState {
	s := &sliderState{min: min, max: max, label: label, format: format}
	s.Set(init)
	return s
}

func (s *sliderState) Get() float64 {
	return s.min + float64(s.f.Value)*(s.max-s.min)
}

func (s *sliderState) Set(v float64) {
	if s.max == s.min {
		s.f.Value = 0
		return
	}
	t := (v - s.min) / (s.max - s.min)
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	s.f.Value = float32(t)
}

func (s *sliderState) Layout(th *material.Theme, gtx layout.Context) layout.Dimensions {
	labelW := gtx.Dp(unit.Dp(80))
	valW := gtx.Dp(unit.Dp(72))
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = labelW
			gtx.Constraints.Max.X = labelW
			lbl := material.Label(th, unit.Sp(13), s.label)
			return lbl.Layout(gtx)
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, material.Slider(th, &s.f).Layout)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = valW
			gtx.Constraints.Max.X = valW
			lbl := material.Label(th, unit.Sp(13), s.format(s.Get()))
			lbl.Alignment = text.End
			return lbl.Layout(gtx)
		}),
	)
}

type UI struct {
	params  Params
	samples []float32
	peak    float64
	rms     float64

	playheadBits atomic.Uint64

	player *Player

	duration, startFreq, endFreq                    *sliderState
	attack, decay, sustain, release, volume         *sliderState
	duty                                            *sliderState
	vibratoDepth, vibratoRate                       *sliderState
	arpSemi1, arpSemi2, arpRate                     *sliderState
	tremoloDepth, tremoloRate                       *sliderState
	wahCenter, wahDepth, wahRate                    *sliderState
	sweepShift, sweepRate                           *sliderState
	noisePitch, metallicPitch, noiseFilterCutoff    *sliderState
	crushBits, crushRate                            *sliderState

	dutyEnabled, vibratoEnabled, arpEnabled, tremoloEnabled widget.Bool
	wahEnabled, sweepEnabled, sweepNegate                   widget.Bool
	noisePitchEnabled, noiseMetallic, noiseFilterEnabled    widget.Bool

	keyFocused bool

	playBtn, backBtn, forwardBtn, mutateBtn, exportBtn widget.Clickable
	saveBtn, loadBtn                        widget.Clickable
	pendingLoad                             atomic.Pointer[Params]
	presetBtns                              map[string]*widget.Clickable
	waveformBtns                            []widget.Clickable
	waveformChoice                          int

	history *History

	autoPlay        widget.Bool
	autoPlayAt      time.Time
	autoPlayPending bool

	reverse  widget.Bool
	eightBit widget.Bool

	w *app.Window // for Invalidate from goroutines

	statusMu    sync.Mutex
	statusMsg   string
	statusUntil time.Time
}

func newUI(player *Player) *UI {
	p := DefaultParams()
	p.SampleRate = sampleRate
	fmtSec := func(v float64) string { return fmt.Sprintf("%.3f s", v) }
	fmtHz := func(v float64) string { return fmt.Sprintf("%.0f Hz", v) }
	fmtAmp := func(v float64) string { return fmt.Sprintf("%.2f", v) }

	ui := &UI{
		params:         p,
		player:         player,
		duration:       newSlider("Duration", 0.05, 3.0, p.Duration, fmtSec),
		startFreq:      newSlider("Start Hz", 20, 4000, p.StartFreq, fmtHz),
		endFreq:        newSlider("End Hz", 20, 4000, p.EndFreq, fmtHz),
		attack:         newSlider("Attack", 0, 1.5, p.Attack, fmtSec),
		decay:          newSlider("Decay", 0, 1.5, p.Decay, fmtSec),
		sustain:        newSlider("Sustain", 0, 1, p.Sustain, fmtAmp),
		release:        newSlider("Release", 0, 1.5, p.Release, fmtSec),
		volume:         newSlider("Volume", 0, 1, p.Volume, fmtAmp),
		duty:           newSlider("Duty", 0.05, 0.95, p.Duty, func(v float64) string { return fmt.Sprintf("%.0f%%", v*100) }),
		vibratoDepth:   newSlider("Depth", 0, 100, p.VibratoDepth, func(v float64) string { return fmt.Sprintf("%.0f c", v) }),
		vibratoRate:    newSlider("Rate", 0.5, 20, p.VibratoRate, func(v float64) string { return fmt.Sprintf("%.1f Hz", v) }),
		arpSemi1:       newSlider("Tone 2", -12, 24, p.ArpSemitone1, func(v float64) string { return fmt.Sprintf("%+.0f st", v) }),
		arpSemi2:       newSlider("Tone 3", -12, 24, p.ArpSemitone2, func(v float64) string { return fmt.Sprintf("%+.0f st", v) }),
		arpRate:        newSlider("Rate", 4, 32, p.ArpRate, func(v float64) string { return fmt.Sprintf("%.0f Hz", v) }),
		tremoloDepth:   newSlider("Depth", 0, 1, p.TremoloDepth, func(v float64) string { return fmt.Sprintf("%.2f", v) }),
		tremoloRate:    newSlider("Rate", 0.5, 20, p.TremoloRate, func(v float64) string { return fmt.Sprintf("%.1f Hz", v) }),
		wahCenter:      newSlider("Center", 200, 3000, p.WahCenter, func(v float64) string { return fmt.Sprintf("%.0f Hz", v) }),
		wahDepth:       newSlider("Depth", 0.5, 4, p.WahDepth, func(v float64) string { return fmt.Sprintf("%.1f oct", v) }),
		wahRate:        newSlider("Rate", 0.5, 15, p.WahRate, func(v float64) string { return fmt.Sprintf("%.1f Hz", v) }),
		sweepShift:     newSlider("Shift", 1, 8, p.SweepShift, func(v float64) string { return fmt.Sprintf("%.0f", math.Round(v)) }),
		sweepRate:      newSlider("Rate", 1, 30, p.SweepRate, func(v float64) string { return fmt.Sprintf("%.0f Hz", v) }),
		noisePitch:     newSlider("Pitch", 100, 22050, p.NoisePitch, func(v float64) string { return fmt.Sprintf("%.0f Hz", v) }),
		metallicPitch:  newSlider("Pitch", 30, 5000, p.MetallicPitch, func(v float64) string { return fmt.Sprintf("%.0f Hz", v) }),
		noiseFilterCutoff: newSlider("Cutoff", 100, 20000, p.NoiseFilterCutoff, func(v float64) string { return fmt.Sprintf("%.0f Hz", v) }),
		crushBits:      newSlider("Bits", 1, 8, float64(p.CrushBits), func(v float64) string { return fmt.Sprintf("%.0f bit", math.Round(v)) }),
		crushRate:      newSlider("Rate", 1000, sampleRate, p.CrushRate, func(v float64) string { return fmt.Sprintf("%.0f Hz", v) }),
		presetBtns:     make(map[string]*widget.Clickable),
		waveformBtns:   make([]widget.Clickable, len(WaveformNames)),
		waveformChoice: int(p.Waveform),
	}
	for _, name := range PresetNames {
		ui.presetBtns[name] = &widget.Clickable{}
	}
	ui.history = NewHistory(p)
	ui.regenerate()
	return ui
}

func (ui *UI) readParams() Params {
	p := ui.params
	p.Waveform = Waveform(ui.waveformChoice)
	p.Duration = ui.duration.Get()
	p.StartFreq = ui.startFreq.Get()
	p.EndFreq = ui.endFreq.Get()
	p.Attack = ui.attack.Get()
	p.Decay = ui.decay.Get()
	p.Sustain = ui.sustain.Get()
	p.Release = ui.release.Get()
	p.Volume = ui.volume.Get()
	p.Reverse = ui.reverse.Value
	p.EightBit = ui.eightBit.Value
	p.CrushBits = int(math.Round(ui.crushBits.Get()))
	p.CrushRate = ui.crushRate.Get()
	p.DutyEnabled = ui.dutyEnabled.Value
	p.Duty = ui.duty.Get()
	p.VibratoEnabled = ui.vibratoEnabled.Value
	p.VibratoDepth = ui.vibratoDepth.Get()
	p.VibratoRate = ui.vibratoRate.Get()
	p.ArpEnabled = ui.arpEnabled.Value
	p.ArpSemitone1 = ui.arpSemi1.Get()
	p.ArpSemitone2 = ui.arpSemi2.Get()
	p.ArpRate = ui.arpRate.Get()
	p.TremoloEnabled = ui.tremoloEnabled.Value
	p.TremoloDepth = ui.tremoloDepth.Get()
	p.TremoloRate = ui.tremoloRate.Get()
	p.WahEnabled = ui.wahEnabled.Value
	p.WahCenter = ui.wahCenter.Get()
	p.WahDepth = ui.wahDepth.Get()
	p.WahRate = ui.wahRate.Get()
	p.SweepEnabled = ui.sweepEnabled.Value
	p.SweepShift = ui.sweepShift.Get()
	p.SweepRate = ui.sweepRate.Get()
	p.SweepNegate = ui.sweepNegate.Value
	p.NoisePitchEnabled = ui.noisePitchEnabled.Value
	p.NoisePitch = ui.noisePitch.Get()
	p.NoiseMetallic = ui.noiseMetallic.Value
	p.MetallicPitch = ui.metallicPitch.Get()
	p.NoiseFilterEnabled = ui.noiseFilterEnabled.Value
	p.NoiseFilterCutoff = ui.noiseFilterCutoff.Get()
	p.SampleRate = sampleRate
	return p
}

func (ui *UI) syncSlidersFromParams() {
	ui.duration.Set(ui.params.Duration)
	ui.startFreq.Set(ui.params.StartFreq)
	ui.endFreq.Set(ui.params.EndFreq)
	ui.attack.Set(ui.params.Attack)
	ui.decay.Set(ui.params.Decay)
	ui.sustain.Set(ui.params.Sustain)
	ui.release.Set(ui.params.Release)
	ui.volume.Set(ui.params.Volume)
	ui.waveformChoice = int(ui.params.Waveform)
	ui.reverse.Value = ui.params.Reverse
	ui.eightBit.Value = ui.params.EightBit
	ui.crushBits.Set(float64(ui.params.CrushBits))
	ui.crushRate.Set(ui.params.CrushRate)
	ui.dutyEnabled.Value = ui.params.DutyEnabled
	ui.duty.Set(ui.params.Duty)
	ui.vibratoEnabled.Value = ui.params.VibratoEnabled
	ui.vibratoDepth.Set(ui.params.VibratoDepth)
	ui.vibratoRate.Set(ui.params.VibratoRate)
	ui.arpEnabled.Value = ui.params.ArpEnabled
	ui.arpSemi1.Set(ui.params.ArpSemitone1)
	ui.arpSemi2.Set(ui.params.ArpSemitone2)
	ui.arpRate.Set(ui.params.ArpRate)
	ui.tremoloEnabled.Value = ui.params.TremoloEnabled
	ui.tremoloDepth.Set(ui.params.TremoloDepth)
	ui.tremoloRate.Set(ui.params.TremoloRate)
	ui.wahEnabled.Value = ui.params.WahEnabled
	ui.wahCenter.Set(ui.params.WahCenter)
	ui.wahDepth.Set(ui.params.WahDepth)
	ui.wahRate.Set(ui.params.WahRate)
	ui.sweepEnabled.Value = ui.params.SweepEnabled
	ui.sweepShift.Set(ui.params.SweepShift)
	ui.sweepRate.Set(ui.params.SweepRate)
	ui.sweepNegate.Value = ui.params.SweepNegate
	ui.noisePitchEnabled.Value = ui.params.NoisePitchEnabled
	ui.noisePitch.Set(ui.params.NoisePitch)
	ui.noiseMetallic.Value = ui.params.NoiseMetallic
	ui.metallicPitch.Set(ui.params.MetallicPitch)
	ui.noiseFilterEnabled.Value = ui.params.NoiseFilterEnabled
	ui.noiseFilterCutoff.Set(ui.params.NoiseFilterCutoff)
}

func (ui *UI) regenerate() {
	ui.samples = Render(ui.params)
	ui.peak, ui.rms = PeakRMS(ui.samples)
}

func (ui *UI) play() {
	if ui.player == nil || len(ui.samples) == 0 {
		return
	}
	ui.player.Play(ui.samples)
}

func (ui *UI) mutate() {
	p := ui.params
	p.StartFreq = clampF(p.StartFreq*(0.7+0.6*rand.Float64()), 20, 4000)
	p.EndFreq = clampF(p.EndFreq*(0.7+0.6*rand.Float64()), 20, 4000)
	p.Attack = clampF(p.Attack*(0.4+1.2*rand.Float64()), 0, 1.5)
	p.Decay = clampF(p.Decay*(0.4+1.2*rand.Float64()), 0, 1.5)
	p.Sustain = clampF(p.Sustain*(0.5+rand.Float64()), 0, 1)
	p.Release = clampF(p.Release*(0.4+1.2*rand.Float64()), 0, 1.5)
	ui.params = p
	ui.syncSlidersFromParams()
}

func (ui *UI) export() {
	samples := ui.samples
	defaultName := fmt.Sprintf("wavman_%s.wav", time.Now().Format("20060102_150405"))
	cwd, _ := os.Getwd()
	go func() {
		path, err := zenity.SelectFileSave(
			zenity.Title("Save WAV"),
			zenity.Filename(filepath.Join(cwd, defaultName)),
			zenity.FileFilters{{Name: "WAV files", Patterns: []string{"*.wav"}, CaseFold: true}},
			zenity.ConfirmOverwrite(),
		)
		if errors.Is(err, zenity.ErrCanceled) {
			return
		}
		if err != nil {
			ui.setStatus("Save dialog failed: "+err.Error(), 6*time.Second)
			return
		}
		if !strings.EqualFold(filepath.Ext(path), ".wav") {
			path += ".wav"
		}
		if err := WriteWAV(path, samples, sampleRate); err != nil {
			ui.setStatus("Save failed: "+err.Error(), 6*time.Second)
			return
		}
		ui.setStatus("Saved: "+path, 6*time.Second)
	}()
}

func (ui *UI) savePreset() {
	p := ui.params
	defaultName := fmt.Sprintf("preset_%s.json", time.Now().Format("20060102_150405"))
	cwd, _ := os.Getwd()
	go func() {
		path, err := zenity.SelectFileSave(
			zenity.Title("Save preset"),
			zenity.Filename(filepath.Join(cwd, defaultName)),
			zenity.FileFilters{{Name: "Preset (JSON)", Patterns: []string{"*.json"}, CaseFold: true}},
			zenity.ConfirmOverwrite(),
		)
		if errors.Is(err, zenity.ErrCanceled) {
			return
		}
		if err != nil {
			ui.setStatus("Save dialog failed: "+err.Error(), 6*time.Second)
			return
		}
		if !strings.EqualFold(filepath.Ext(path), ".json") {
			path += ".json"
		}
		if err := SaveParams(path, p); err != nil {
			ui.setStatus("Save preset failed: "+err.Error(), 6*time.Second)
			return
		}
		ui.setStatus("Saved preset: "+path, 6*time.Second)
	}()
}

func (ui *UI) loadPreset() {
	cwd, _ := os.Getwd()
	go func() {
		path, err := zenity.SelectFile(
			zenity.Title("Load preset"),
			zenity.Filename(cwd),
			zenity.FileFilters{{Name: "Preset (JSON)", Patterns: []string{"*.json"}, CaseFold: true}},
		)
		if errors.Is(err, zenity.ErrCanceled) {
			return
		}
		if err != nil {
			ui.setStatus("Load dialog failed: "+err.Error(), 6*time.Second)
			return
		}
		p, err := LoadParams(path)
		if err != nil {
			ui.setStatus("Load failed: "+err.Error(), 6*time.Second)
			return
		}
		ui.pendingLoad.Store(&p)
		ui.setStatus("Loaded preset: "+path, 6*time.Second)
		if ui.w != nil {
			ui.w.Invalidate()
		}
	}()
}

func (ui *UI) setStatus(msg string, dur time.Duration) {
	ui.statusMu.Lock()
	ui.statusMsg = msg
	ui.statusUntil = time.Now().Add(dur)
	ui.statusMu.Unlock()
	if ui.w != nil {
		ui.w.Invalidate()
	}
}

func (ui *UI) getStatus() (string, time.Time) {
	ui.statusMu.Lock()
	defer ui.statusMu.Unlock()
	return ui.statusMsg, ui.statusUntil
}

func (ui *UI) getPlayhead() float64 {
	return math.Float64frombits(ui.playheadBits.Load())
}

func (ui *UI) setPlayhead(v float64) {
	ui.playheadBits.Store(math.Float64bits(v))
}

func clampF(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func (ui *UI) layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	// Global key listener: Space = Play. Register the UI as an event target
	// and pull any space-key presses. Focus is requested once at startup.
	event.Op(gtx.Ops, ui)
	for {
		ev, ok := gtx.Event(key.Filter{Focus: ui, Name: key.NameSpace})
		if !ok {
			break
		}
		if ke, ok := ev.(key.Event); ok && ke.State == key.Press {
			ui.play()
		}
	}
	if !ui.keyFocused {
		gtx.Execute(key.FocusCmd{Tag: ui})
		ui.keyFocused = true
	}

	// Apply a pending preset load (handed off from a goroutine via atomic).
	if loaded := ui.pendingLoad.Swap(nil); loaded != nil {
		ui.history.SetCurrent(ui.readParams())
		ui.params = *loaded
		ui.syncSlidersFromParams()
		ui.regenerate()
		ui.history.Push(ui.params)
		ui.play()
	}

	immediatePlay := false
	pushOnChange := false
	for i := range ui.waveformBtns {
		if ui.waveformBtns[i].Clicked(gtx) {
			ui.waveformChoice = i
			pushOnChange = true
			if ui.autoPlay.Value {
				immediatePlay = true
			}
		}
	}
	for _, name := range PresetNames {
		if ui.presetBtns[name].Clicked(gtx) {
			ui.history.SetCurrent(ui.readParams())
			ui.params = ApplyPreset(name)
			ui.syncSlidersFromParams()
			ui.regenerate()
			ui.history.Push(ui.params)
			ui.play()
		}
	}
	if ui.playBtn.Clicked(gtx) {
		ui.play()
	}
	if ui.backBtn.Clicked(gtx) && ui.history.CanBack() {
		ui.history.SetCurrent(ui.readParams())
		ui.params = ui.history.Back()
		ui.syncSlidersFromParams()
		ui.regenerate()
		ui.play()
	}
	if ui.forwardBtn.Clicked(gtx) && !ui.history.AtEnd() {
		ui.history.SetCurrent(ui.readParams())
		ui.params = ui.history.Forward()
		ui.syncSlidersFromParams()
		ui.regenerate()
		ui.play()
	}
	if ui.mutateBtn.Clicked(gtx) {
		ui.history.SetCurrent(ui.readParams())
		ui.mutate()
		ui.regenerate()
		ui.history.Push(ui.params)
		ui.play()
	}
	if ui.exportBtn.Clicked(gtx) {
		ui.export()
	}
	if ui.saveBtn.Clicked(gtx) {
		ui.savePreset()
	}
	if ui.loadBtn.Clicked(gtx) {
		ui.loadPreset()
	}

	if newP := ui.readParams(); newP != ui.params {
		ui.params = newP
		ui.regenerate()
		if pushOnChange {
			ui.history.Push(ui.params)
		} else {
			ui.history.SetCurrent(ui.params)
		}
		if ui.autoPlay.Value {
			if immediatePlay {
				ui.play()
				ui.autoPlayPending = false
			} else {
				ui.autoPlayAt = time.Now().Add(180 * time.Millisecond)
				ui.autoPlayPending = true
			}
		}
	}

	if !ui.autoPlay.Value {
		ui.autoPlayPending = false
	}
	if ui.autoPlayPending {
		if !time.Now().Before(ui.autoPlayAt) {
			ui.play()
			ui.autoPlayPending = false
		} else {
			gtx.Execute(op.InvalidateCmd{At: ui.autoPlayAt})
		}
	}

	// background
	paint.Fill(gtx.Ops, color.NRGBA{R: 30, G: 33, B: 45, A: 255})

	return layout.Inset{Top: unit.Dp(10), Bottom: unit.Dp(10), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical, Spacing: layout.SpaceBetween}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.topBar(gtx, th) }),
				layout.Rigid(spacer(8)),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
						layout.Flexed(0.42, func(gtx layout.Context) layout.Dimensions { return ui.leftPanel(gtx, th) }),
						layout.Rigid(spacer(10)),
						layout.Flexed(0.58, func(gtx layout.Context) layout.Dimensions { return ui.rightPanel(gtx, th) }),
					)
				}),
				layout.Rigid(spacer(6)),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.statusBar(gtx, th) }),
			)
		},
	)
}

func spacer(dp int) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return layout.Spacer{Height: unit.Dp(float32(dp)), Width: unit.Dp(float32(dp))}.Layout(gtx)
	}
}

func (ui *UI) topBar(gtx layout.Context, th *material.Theme) layout.Dimensions {
	children := []layout.FlexChild{
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return material.Label(th, unit.Sp(14), "Wave: ").Layout(gtx)
		}),
	}
	for i, name := range WaveformNames {
		i := i
		name := name
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			btn := material.Button(th, &ui.waveformBtns[i], name)
			if ui.waveformChoice == i {
				btn.Background = color.NRGBA{R: 80, G: 130, B: 200, A: 255}
			} else {
				btn.Background = color.NRGBA{R: 55, G: 60, B: 80, A: 255}
			}
			btn.CornerRadius = unit.Dp(4)
			btn.Inset = layout.Inset{Top: 6, Bottom: 6, Left: 10, Right: 10}
			return layout.Inset{Right: unit.Dp(4)}.Layout(gtx, btn.Layout)
		}))
	}
	children = append(children,
		layout.Flexed(1, spacer(0)),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			cb := material.CheckBox(th, &ui.reverse, "Reverse")
			cb.Color = color.NRGBA{R: 220, G: 225, B: 235, A: 255}
			return layout.Inset{Right: unit.Dp(12)}.Layout(gtx, cb.Layout)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			cb := material.CheckBox(th, &ui.autoPlay, "Auto-play")
			cb.Color = color.NRGBA{R: 220, G: 225, B: 235, A: 255}
			return layout.Inset{Right: unit.Dp(12)}.Layout(gtx, cb.Layout)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			btn := material.Button(th, &ui.playBtn, "▶  Play")
			btn.Background = color.NRGBA{R: 70, G: 160, B: 90, A: 255}
			return layout.Inset{Right: unit.Dp(6)}.Layout(gtx, btn.Layout)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			btn := material.Button(th, &ui.backBtn, "◄")
			if ui.history.CanBack() {
				btn.Background = color.NRGBA{R: 180, G: 130, B: 60, A: 255}
			} else {
				btn.Background = color.NRGBA{R: 80, G: 80, B: 90, A: 255}
			}
			btn.Inset = layout.Inset{Top: 6, Bottom: 6, Left: 12, Right: 12}
			return layout.Inset{Right: unit.Dp(2)}.Layout(gtx, btn.Layout)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			btn := material.Button(th, &ui.forwardBtn, "►")
			if !ui.history.AtEnd() {
				btn.Background = color.NRGBA{R: 180, G: 130, B: 60, A: 255}
			} else {
				btn.Background = color.NRGBA{R: 80, G: 80, B: 90, A: 255}
			}
			btn.Inset = layout.Inset{Top: 6, Bottom: 6, Left: 12, Right: 12}
			return layout.Inset{Right: unit.Dp(6)}.Layout(gtx, btn.Layout)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			btn := material.Button(th, &ui.mutateBtn, "Mutate")
			btn.Background = color.NRGBA{R: 180, G: 130, B: 60, A: 255}
			btn.Inset = layout.Inset{Top: 6, Bottom: 6, Left: 12, Right: 12}
			return layout.Inset{Right: unit.Dp(6)}.Layout(gtx, btn.Layout)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			btn := material.Button(th, &ui.exportBtn, "Export WAV")
			btn.Background = color.NRGBA{R: 90, G: 110, B: 170, A: 255}
			return btn.Layout(gtx)
		}),
	)
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx, children...)
}

func (ui *UI) leftPanel(gtx layout.Context, th *material.Theme) layout.Dimensions {
	return cardLayout(gtx, th, "", func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(sectionTitle(th, "Presets")),
			layout.Rigid(spacer(4)),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				cols := 3
				rows := (len(PresetNames) + cols - 1) / cols
				items := make([]layout.FlexChild, 0, rows)
				for r := 0; r < rows; r++ {
					r := r
					items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						rowItems := make([]layout.FlexChild, 0, cols)
						for c := 0; c < cols; c++ {
							idx := r*cols + c
							if idx >= len(PresetNames) {
								rowItems = append(rowItems, layout.Flexed(1, spacer(0)))
								continue
							}
							name := PresetNames[idx]
							rowItems = append(rowItems, layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
								btn := material.Button(th, ui.presetBtns[name], name)
								btn.Background = color.NRGBA{R: 60, G: 70, B: 95, A: 255}
								btn.CornerRadius = unit.Dp(4)
								return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(2), Left: unit.Dp(2), Right: unit.Dp(2)}.Layout(gtx, btn.Layout)
							}))
						}
						return layout.Flex{Axis: layout.Horizontal}.Layout(gtx, rowItems...)
					}))
				}
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx, items...)
			}),
			layout.Rigid(spacer(6)),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						btn := material.Button(th, &ui.saveBtn, "Save…")
						btn.Background = color.NRGBA{R: 90, G: 110, B: 170, A: 255}
						btn.CornerRadius = unit.Dp(4)
						return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(2), Left: unit.Dp(2), Right: unit.Dp(2)}.Layout(gtx, btn.Layout)
					}),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						btn := material.Button(th, &ui.loadBtn, "Load…")
						btn.Background = color.NRGBA{R: 90, G: 110, B: 170, A: 255}
						btn.CornerRadius = unit.Dp(4)
						return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(2), Left: unit.Dp(2), Right: unit.Dp(2)}.Layout(gtx, btn.Layout)
					}),
				)
			}),
			layout.Rigid(spacer(12)),
			layout.Rigid(sectionTitle(th, "Parameters")),
			layout.Rigid(spacer(4)),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.duration.Layout(th, gtx) }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.startFreq.Layout(th, gtx) }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.endFreq.Layout(th, gtx) }),
					layout.Rigid(spacer(6)),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.attack.Layout(th, gtx) }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.decay.Layout(th, gtx) }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.sustain.Layout(th, gtx) }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.release.Layout(th, gtx) }),
					layout.Rigid(spacer(6)),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.volume.Layout(th, gtx) }),
				)
			}),
			layout.Rigid(spacer(12)),
			layout.Rigid(sectionTitle(th, "Modulation")),
			layout.Rigid(spacer(4)),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.modulationContent(gtx, th) }),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if ui.waveformChoice != int(Noise) {
					return layout.Dimensions{}
				}
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(spacer(12)),
					layout.Rigid(sectionTitle(th, "Noise")),
					layout.Rigid(spacer(4)),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.noiseContent(gtx, th) }),
				)
			}),
		)
	})
}

func (ui *UI) noiseContent(gtx layout.Context, th *material.Theme) layout.Dimensions {
	children := []layout.FlexChild{
		layout.Rigid(moduleHeader(th, &ui.noisePitchEnabled, "Pitch (sample-and-hold)")),
	}
	if ui.noisePitchEnabled.Value {
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.noisePitch.Layout(th, gtx) }))
	}
	children = append(children,
		layout.Rigid(spacer(6)),
		layout.Rigid(moduleHeader(th, &ui.noiseMetallic, "Metallic (NES short LFSR)")),
	)
	if ui.noiseMetallic.Value {
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.metallicPitch.Layout(th, gtx) }))
	}
	children = append(children,
		layout.Rigid(spacer(6)),
		layout.Rigid(moduleHeader(th, &ui.noiseFilterEnabled, "Low-pass filter")),
	)
	if ui.noiseFilterEnabled.Value {
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.noiseFilterCutoff.Layout(th, gtx) }))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

func moduleHeader(th *material.Theme, b *widget.Bool, label string) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		cb := material.CheckBox(th, b, label)
		cb.Color = color.NRGBA{R: 200, G: 215, B: 235, A: 255}
		cb.Font.Weight = font.Bold
		return cb.Layout(gtx)
	}
}

func (ui *UI) modulationContent(gtx layout.Context, th *material.Theme) layout.Dimensions {
	children := []layout.FlexChild{
		layout.Rigid(moduleHeader(th, &ui.dutyEnabled, "Duty cycle (Square only)")),
	}
	if ui.dutyEnabled.Value {
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.duty.Layout(th, gtx) }))
	}
	children = append(children,
		layout.Rigid(spacer(6)),
		layout.Rigid(moduleHeader(th, &ui.vibratoEnabled, "Vibrato")),
	)
	if ui.vibratoEnabled.Value {
		children = append(children,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.vibratoDepth.Layout(th, gtx) }),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.vibratoRate.Layout(th, gtx) }),
		)
	}
	children = append(children,
		layout.Rigid(spacer(6)),
		layout.Rigid(moduleHeader(th, &ui.arpEnabled, "Arpeggio")),
	)
	if ui.arpEnabled.Value {
		children = append(children,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.arpSemi1.Layout(th, gtx) }),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.arpSemi2.Layout(th, gtx) }),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.arpRate.Layout(th, gtx) }),
		)
	}
	children = append(children,
		layout.Rigid(spacer(6)),
		layout.Rigid(moduleHeader(th, &ui.sweepEnabled, "Pulse sweep")),
	)
	if ui.sweepEnabled.Value {
		children = append(children,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.sweepShift.Layout(th, gtx) }),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.sweepRate.Layout(th, gtx) }),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				cb := material.CheckBox(th, &ui.sweepNegate, "Sweep up")
				cb.Color = color.NRGBA{R: 220, G: 225, B: 235, A: 255}
				return layout.Inset{Left: unit.Dp(80), Top: unit.Dp(2)}.Layout(gtx, cb.Layout)
			}),
		)
	}
	children = append(children,
		layout.Rigid(spacer(6)),
		layout.Rigid(moduleHeader(th, &ui.tremoloEnabled, "Tremolo")),
	)
	if ui.tremoloEnabled.Value {
		children = append(children,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.tremoloDepth.Layout(th, gtx) }),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.tremoloRate.Layout(th, gtx) }),
		)
	}
	children = append(children,
		layout.Rigid(spacer(6)),
		layout.Rigid(moduleHeader(th, &ui.wahEnabled, "Wah-wah")),
	)
	if ui.wahEnabled.Value {
		children = append(children,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.wahCenter.Layout(th, gtx) }),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.wahDepth.Layout(th, gtx) }),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.wahRate.Layout(th, gtx) }),
		)
	}
	children = append(children,
		layout.Rigid(spacer(6)),
		layout.Rigid(moduleHeader(th, &ui.eightBit, "8-bit (bitcrush)")),
	)
	if ui.eightBit.Value {
		children = append(children,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.crushBits.Layout(th, gtx) }),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.crushRate.Layout(th, gtx) }),
		)
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

func (ui *UI) rightPanel(gtx layout.Context, th *material.Theme) layout.Dimensions {
	return cardLayout(gtx, th, "", func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(sectionTitle(th, "Waveform")),
			layout.Rigid(spacer(4)),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.Y = gtx.Dp(unit.Dp(220))
				gtx.Constraints.Max.Y = gtx.Dp(unit.Dp(220))
				return WaveformView{Samples: ui.samples, Playhead: ui.getPlayhead()}.Layout(gtx)
			}),
			layout.Rigid(spacer(10)),
			layout.Rigid(sectionTitle(th, "ADSR envelope")),
			layout.Rigid(spacer(4)),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.Y = gtx.Dp(unit.Dp(120))
				gtx.Constraints.Max.Y = gtx.Dp(unit.Dp(120))
				return EnvelopeView{Params: ui.params}.Layout(gtx)
			}),
			layout.Rigid(spacer(10)),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				txt := fmt.Sprintf("Peak: %.3f    RMS: %.3f    Samples: %d    (%d Hz, %.2f s)",
					ui.peak, ui.rms, len(ui.samples), sampleRate, ui.params.Duration)
				return material.Label(th, unit.Sp(13), txt).Layout(gtx)
			}),
		)
	})
}

func (ui *UI) statusBar(gtx layout.Context, th *material.Theme) layout.Dimensions {
	msg, until := ui.getStatus()
	if msg != "" && time.Now().Before(until) {
		lbl := material.Label(th, unit.Sp(12), msg)
		lbl.Color = color.NRGBA{R: 200, G: 220, B: 240, A: 255}
		return lbl.Layout(gtx)
	}
	if ui.player == nil {
		lbl := material.Label(th, unit.Sp(12), "(audio playback disabled)")
		lbl.Color = color.NRGBA{R: 200, G: 120, B: 120, A: 255}
		return lbl.Layout(gtx)
	}
	return layout.Dimensions{Size: image.Point{Y: gtx.Dp(unit.Dp(16))}}
}

func sectionTitle(th *material.Theme, txt string) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		lbl := material.Label(th, unit.Sp(14), txt)
		lbl.Color = color.NRGBA{R: 180, G: 200, B: 230, A: 255}
		lbl.Font.Weight = font.Bold
		return lbl.Layout(gtx)
	}
}

func cardLayout(gtx layout.Context, th *material.Theme, title string, content layout.Widget) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		// Background panel
		size := gtx.Constraints.Max
		paintFill(gtx, image.Rect(0, 0, size.X, size.Y), color.NRGBA{R: 40, G: 44, B: 60, A: 255})
		return layout.Inset{Top: unit.Dp(10), Bottom: unit.Dp(10), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, content)
	})
}

func main() {
	go func() {
		w := new(app.Window)
		w.Option(
			app.Title("wavman — WAV sound generator"),
			app.Size(unit.Dp(1100), unit.Dp(820)),
		)
		if err := run(w); err != nil {
			log.Fatal(err)
		}
		os.Exit(0)
	}()
	app.Main()
}

func run(w *app.Window) error {
	th := material.NewTheme()
	th.Shaper = text.NewShaper(text.WithCollection(gofont.Collection()))

	player, err := GetPlayer(sampleRate)
	if err != nil {
		log.Printf("audio playback unavailable: %v", err)
	}

	ui := newUI(player)
	ui.w = w
	if player != nil {
		player.SetPlayheadCallback(func(p float64) {
			ui.setPlayhead(p)
			w.Invalidate()
		})
	}

	var ops op.Ops
	for {
		switch e := w.Event().(type) {
		case app.DestroyEvent:
			return e.Err
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			ui.layout(gtx, th)
			e.Frame(gtx.Ops)
		}
	}
}
