package main

import "math/rand"

var PresetNames = []string{"Pickup", "Laser", "Explosion", "Hit", "Jump", "Blip"}

func ApplyPreset(name string) Params {
	p := DefaultParams()
	switch name {
	case "Pickup":
		p.Waveform = Square
		p.StartFreq = 600 + rand.Float64()*400
		p.EndFreq = p.StartFreq * (1.8 + rand.Float64())
		p.Duration = 0.18
		p.Attack = 0.0
		p.Decay = 0.04
		p.Sustain = 0.75
		p.Release = 0.10
		p.Volume = 0.8
	case "Laser":
		p.Waveform = Saw
		p.StartFreq = 1400 + rand.Float64()*600
		p.EndFreq = 180 + rand.Float64()*120
		p.Duration = 0.28
		p.Attack = 0.0
		p.Decay = 0.05
		p.Sustain = 0.65
		p.Release = 0.18
		p.Volume = 0.75
	case "Explosion":
		p.Waveform = Noise
		p.StartFreq = 1
		p.EndFreq = 1
		p.Duration = 0.55 + rand.Float64()*0.2
		p.Attack = 0.0
		p.Decay = 0.18
		p.Sustain = 0.55
		p.Release = 0.35
		p.Volume = 0.85
	case "Hit":
		p.Waveform = Noise
		p.StartFreq = 1
		p.EndFreq = 1
		p.Duration = 0.13
		p.Attack = 0.0
		p.Decay = 0.04
		p.Sustain = 0.4
		p.Release = 0.07
		p.Volume = 0.85
	case "Jump":
		p.Waveform = Square
		p.StartFreq = 200 + rand.Float64()*120
		p.EndFreq = p.StartFreq * (1.6 + rand.Float64()*0.6)
		p.Duration = 0.22
		p.Attack = 0.01
		p.Decay = 0.06
		p.Sustain = 0.8
		p.Release = 0.08
		p.Volume = 0.8
	case "Blip":
		p.Waveform = Triangle
		p.StartFreq = 800 + rand.Float64()*500
		p.EndFreq = p.StartFreq
		p.Duration = 0.09
		p.Attack = 0.0
		p.Decay = 0.02
		p.Sustain = 0.85
		p.Release = 0.04
		p.Volume = 0.8
	}
	p.Seed = rand.Int63() // fresh noise for each preset application
	return p
}
