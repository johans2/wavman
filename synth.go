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

	Reverse  bool
	EightBit bool

	DutyEnabled bool
	Duty        float64 // 0.05..0.95 for Square wave; 0.5 = symmetric

	VibratoEnabled bool
	VibratoDepth   float64 // cents (semitone = 100 cents)
	VibratoRate    float64 // Hz

	ArpEnabled   bool
	ArpSemitone1 float64 // semitones offset for step 2
	ArpSemitone2 float64 // semitones offset for step 3
	ArpRate      float64 // Hz — steps per second (3 steps per full cycle)

	TremoloEnabled bool
	TremoloDepth   float64 // 0..1 — fraction of volume the LFO can dip
	TremoloRate    float64 // Hz

	NoisePitchEnabled bool
	NoisePitch        float64 // Hz; sample-and-hold rate. Lower = lower-pitched noise.

	NoiseMetallic bool    // when true, use NES 15-bit short-mode LFSR instead of uniform random.
	MetallicPitch float64 // Hz; fundamental of the metallic LFSR tone (LFSR clocked at 93 × this).

	NoiseFilterEnabled bool
	NoiseFilterCutoff  float64 // Hz; one-pole low-pass cutoff.
}

func DefaultParams() Params {
	return Params{
		Waveform:     Sine,
		Duration:     0.5,
		SampleRate:   44100,
		StartFreq:    440,
		EndFreq:      440,
		Attack:       0.01,
		Decay:        0.10,
		Sustain:      0.70,
		Release:      0.20,
		Volume:       0.80,
		Duty:              0.5,
		VibratoDepth:      30,
		VibratoRate:       6,
		ArpSemitone1:      4,
		ArpSemitone2:      7,
		ArpRate:           16,
		TremoloDepth:      0.5,
		TremoloRate:       8,
		NoisePitch:        4000,
		MetallicPitch:     3800,
		NoiseFilterCutoff: 2000,
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

	// Noise generator state (persists across samples).
	var (
		lfsr            uint16 = 1
		lfsrPhase       float64
		noiseHoldVal    float64
		noiseHoldRemain int
		noiseFilterY    float64
	)

	for i := 0; i < n; i++ {
		t := float64(i) / float64(p.SampleRate)
		progress := t / p.Duration
		freq := p.StartFreq + (p.EndFreq-p.StartFreq)*progress

		if p.VibratoEnabled && p.VibratoDepth > 0 {
			cents := p.VibratoDepth * math.Sin(2*math.Pi*p.VibratoRate*t)
			freq *= math.Pow(2, cents/1200)
		}
		if p.ArpEnabled && p.ArpRate > 0 {
			step := int(math.Floor(t*p.ArpRate)) % 3
			var semis float64
			switch step {
			case 1:
				semis = p.ArpSemitone1
			case 2:
				semis = p.ArpSemitone2
			}
			if semis != 0 {
				freq *= math.Pow(2, semis/12)
			}
		}

		var sample float64
		switch p.Waveform {
		case Sine:
			sample = math.Sin(phase)
		case Square:
			duty := 0.5
			if p.DutyEnabled {
				duty = p.Duty
			}
			if math.Mod(phase, twoPi)/twoPi < duty {
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
			if p.NoiseMetallic {
				// NES short-mode LFSR: 15-bit shift register, feedback = bit0 XOR bit6,
				// period 93. Output fundamental = LFSR clock / 93. A fractional phase
				// accumulator advances the LFSR by clockHz/sampleRate steps per output
				// sample, so the metallic pitch is continuously controllable from a slow
				// rattle up to a high clang regardless of output sample rate.
				lfsrPhase += 93.0 * p.MetallicPitch / float64(p.SampleRate)
				for lfsrPhase >= 1 {
					bit := (lfsr ^ (lfsr >> 6)) & 1
					lfsr = (lfsr >> 1) | (bit << 14)
					lfsrPhase--
				}
				if lfsr&1 == 0 {
					sample = 1
				} else {
					sample = -1
				}
			} else {
				holdSamples := 1
				if p.NoisePitchEnabled && p.NoisePitch > 0 {
					holdSamples = int(float64(p.SampleRate) / p.NoisePitch)
					if holdSamples < 1 {
						holdSamples = 1
					}
				}
				if noiseHoldRemain <= 0 {
					noiseHoldVal = rand.Float64()*2 - 1
					noiseHoldRemain = holdSamples
				}
				sample = noiseHoldVal
				noiseHoldRemain--
			}

			if p.NoiseFilterEnabled && p.NoiseFilterCutoff > 0 {
				// One-pole IIR low-pass: y[n] = y[n-1] + a*(x[n] - y[n-1])
				rc := 1.0 / (2 * math.Pi * p.NoiseFilterCutoff)
				dt := 1.0 / float64(p.SampleRate)
				a := dt / (rc + dt)
				noiseFilterY += a * (sample - noiseFilterY)
				sample = noiseFilterY
			}
		}

		env := Envelope(t, p.Duration, p.Attack, p.Decay, p.Sustain, p.Release)
		amp := p.Volume
		if p.TremoloEnabled && p.TremoloDepth > 0 {
			lfo := 0.5 * (1 + math.Sin(2*math.Pi*p.TremoloRate*t))
			amp *= 1 - p.TremoloDepth + p.TremoloDepth*lfo
		}
		out[i] = float32(sample * env * amp)

		phase += twoPi * freq / float64(p.SampleRate)
		if phase > twoPi {
			phase -= twoPi
		}
	}
	if p.EightBit {
		// 4-bit quantization (16 levels) — matches the NES 2A03's effective DAC
		// depth and is the iconic stepped/chiptune character.
		const bits = 4
		step := float32(2.0 / float64(int(1)<<bits))
		for i := range out {
			v := float32(math.Round(float64(out[i]/step))) * step
			if v > 1 {
				v = 1
			} else if v < -1 {
				v = -1
			}
			out[i] = v
		}
	}
	if p.Reverse {
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
			out[i], out[j] = out[j], out[i]
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
