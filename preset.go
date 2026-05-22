package main

import (
	"encoding/json"
	"os"
)

// SaveParams writes p to filename as pretty-printed JSON. SampleRate is
// zeroed in the output because it's an app-wide invariant (44.1 kHz),
// not part of the preset itself.
func SaveParams(filename string, p Params) error {
	p.SampleRate = 0
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filename, b, 0o644)
}

// LoadParams reads filename and unmarshals it into Params. SampleRate is
// always forced to the app's rate so presets remain portable.
func LoadParams(filename string) (Params, error) {
	b, err := os.ReadFile(filename)
	if err != nil {
		return Params{}, err
	}
	var p Params
	if err := json.Unmarshal(b, &p); err != nil {
		return Params{}, err
	}
	p.SampleRate = sampleRate
	return p, nil
}
