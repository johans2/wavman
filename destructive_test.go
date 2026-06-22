package main

import (
	"math"
	"testing"
)

// maxDiff returns the largest absolute sample difference between two buffers.
func maxDiff(a, b []float32) float64 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	var m float64
	for i := 0; i < n; i++ {
		d := math.Abs(float64(a[i]) - float64(b[i]))
		if d > m {
			m = d
		}
	}
	if len(a) != len(b) {
		return math.Inf(1)
	}
	return m
}

// Render must be a pure function of Params for every waveform — including
// Noise, which now seeds its PRNG from Params.Seed.
func TestRenderDeterminism(t *testing.T) {
	for _, w := range []Waveform{Sine, Square, Saw, Triangle, Noise} {
		p := DefaultParams()
		p.SampleRate = sampleRate
		p.Waveform = w
		if d := maxDiff(Render(p), Render(p)); d != 0 {
			t.Errorf("%s: identical params gave different output (maxDiff %g)", WaveformNames[w], d)
		}
	}
}

// Different seeds must give different noise; the same seed must repeat exactly.
func TestNoiseSeed(t *testing.T) {
	p := DefaultParams()
	p.SampleRate = sampleRate
	p.Waveform = Noise

	p.Seed = 1
	a := Render(p)
	p.Seed = 2
	b := Render(p)
	p.Seed = 1
	c := Render(p)

	if maxDiff(a, c) != 0 {
		t.Error("same seed produced different noise")
	}
	if maxDiff(a, b) == 0 {
		t.Error("different seeds produced identical noise")
	}
}

// Enable an effect then disable it: does the output return to baseline?
// This simulates the user's exact complaint at the Params level.
func TestToggleEffectReturnsToBaseline(t *testing.T) {
	cases := []struct {
		name   string
		toggle func(*Params, bool)
	}{
		{"Vibrato", func(p *Params, on bool) { p.VibratoEnabled = on }},
		{"Arpeggio", func(p *Params, on bool) { p.ArpEnabled = on }},
		{"Tremolo", func(p *Params, on bool) { p.TremoloEnabled = on }},
		{"Wah", func(p *Params, on bool) { p.WahEnabled = on }},
		{"PulseSweep", func(p *Params, on bool) { p.SweepEnabled = on }},
		{"EightBit", func(p *Params, on bool) { p.EightBit = on }},
		{"Duty", func(p *Params, on bool) { p.DutyEnabled = on }},
	}
	for _, w := range []Waveform{Square, Noise} {
		for _, c := range cases {
			p := DefaultParams()
			p.SampleRate = sampleRate
			p.Waveform = w
			base := Render(p)

			on := p
			c.toggle(&on, true)
			_ = Render(on)

			off := p
			c.toggle(&off, false)
			after := Render(off)

			if d := maxDiff(base, after); d != 0 {
				t.Errorf("%s/%s: on→off did not return to baseline (maxDiff %g)", WaveformNames[w], c.name, d)
			}
		}
	}
}

// The UI stores slider values as float32 in [0,1] and maps back to the real
// range. Does a value survive a Set→Get roundtrip? This is the other suspect.
func TestSliderRoundTrip(t *testing.T) {
	s := newSlider("StartHz", 20, 4000, 440, func(float64) string { return "" })
	got := s.Get()
	t.Logf("Set(440) -> Get() = %.10f  (drift %g)", got, math.Abs(got-440))

	// Re-set with the value we read back, repeatedly — does it keep drifting?
	v := got
	for i := 0; i < 5; i++ {
		s.Set(v)
		v = s.Get()
	}
	t.Logf("after 5 Set/Get cycles = %.10f", v)
}
