package main

import (
	"math"
	"math/rand"
)

type Waveform int

const (
	Sine Waveform = iota
	Square
	Saw
	Triangle
	Noise
)

var WaveformNames = []string{"Sine", "Square", "Saw", "Triangle", "Noise"}

func WaveformFromName(s string) Waveform {
	for i, n := range WaveformNames {
		if n == s {
			return Waveform(i)
		}
	}
	return Sine
}

type Params struct {
	Waveform   Waveform
	Duration   float64 // seconds
	SampleRate int

	StartFreq float64 // Hz
	EndFreq   float64 // Hz (for pitch sweep)

	Attack  float64 // seconds
	Decay   float64 // seconds
	Sustain float64 // amplitude 0..1
	Release float64 // seconds

	Volume float64 // 0..1
}

func DefaultParams() Params {
	return Params{
		Waveform:   Sine,
		Duration:   0.5,
		SampleRate: 44100,
		StartFreq:  440,
		EndFreq:    440,
		Attack:     0.01,
		Decay:      0.10,
		Sustain:    0.70,
		Release:    0.20,
		Volume:     0.80,
	}
}

// Envelope returns the amplitude multiplier at time t for total duration d.
// Sustain is the level held between decay end and release start. If A+D+R > d,
// segments are scaled proportionally so the envelope always fits the duration.
func Envelope(t, d, a, dec, s, r float64) float64 {
	if t < 0 || t > d {
		return 0
	}
	// Scale ADR to fit if needed
	adr := a + dec + r
	if adr > d {
		scale := d / adr
		a *= scale
		dec *= scale
		r *= scale
	}
	sustainEnd := d - r
	switch {
	case t < a:
		if a <= 0 {
			return 1
		}
		return t / a
	case t < a+dec:
		if dec <= 0 {
			return s
		}
		return 1 - (1-s)*(t-a)/dec
	case t < sustainEnd:
		return s
	default:
		if r <= 0 {
			return 0
		}
		rt := (t - sustainEnd) / r
		if rt > 1 {
			rt = 1
		}
		return s * (1 - rt)
	}
}

// Render synthesises the sound into a mono float32 buffer in [-1, 1].
func Render(p Params) []float32 {
	n := int(p.Duration * float64(p.SampleRate))
	if n <= 0 {
		return nil
	}
	out := make([]float32, n)
	phase := 0.0
	twoPi := 2 * math.Pi

	for i := 0; i < n; i++ {
		t := float64(i) / float64(p.SampleRate)
		progress := t / p.Duration
		freq := p.StartFreq + (p.EndFreq-p.StartFreq)*progress

		var sample float64
		switch p.Waveform {
		case Sine:
			sample = math.Sin(phase)
		case Square:
			if math.Mod(phase, twoPi) < math.Pi {
				sample = 1
			} else {
				sample = -1
			}
		case Saw:
			ph := math.Mod(phase, twoPi) / twoPi
			sample = 2*ph - 1
		case Triangle:
			ph := math.Mod(phase, twoPi) / twoPi
			if ph < 0.5 {
				sample = -1 + 4*ph
			} else {
				sample = 3 - 4*ph
			}
		case Noise:
			sample = rand.Float64()*2 - 1
		}

		env := Envelope(t, p.Duration, p.Attack, p.Decay, p.Sustain, p.Release)
		out[i] = float32(sample * env * p.Volume)

		phase += twoPi * freq / float64(p.SampleRate)
		if phase > twoPi {
			phase -= twoPi
		}
	}
	return out
}

// PeakRMS returns the peak absolute value and RMS of the buffer.
func PeakRMS(samples []float32) (peak, rms float64) {
	if len(samples) == 0 {
		return 0, 0
	}
	var sumSq float64
	for _, s := range samples {
		a := math.Abs(float64(s))
		if a > peak {
			peak = a
		}
		sumSq += float64(s) * float64(s)
	}
	rms = math.Sqrt(sumSq / float64(len(samples)))
	return
}
