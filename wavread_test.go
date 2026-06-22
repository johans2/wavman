package main

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

// roundtrip: write a 16-bit mono 44.1k WAV via the existing writer, read it
// back, and confirm samples survive within 16-bit quantization error.
func TestReadWAVRoundTrip(t *testing.T) {
	in := make([]float32, 1000)
	for i := range in {
		in[i] = float32(math.Sin(2 * math.Pi * 440 * float64(i) / sampleRate))
	}
	path := filepath.Join(t.TempDir(), "rt.wav")
	if err := WriteWAV(path, in, sampleRate); err != nil {
		t.Fatal(err)
	}
	out, trunc, err := ReadWAVMono44k(path)
	if err != nil {
		t.Fatal(err)
	}
	if trunc {
		t.Fatal("unexpected truncation")
	}
	if len(out) != len(in) {
		t.Fatalf("length mismatch: got %d want %d", len(out), len(in))
	}
	for i := range in {
		if d := math.Abs(float64(in[i] - out[i])); d > 2.0/32767 {
			t.Fatalf("sample %d off by %g (in=%g out=%g)", i, d, in[i], out[i])
		}
	}
}

// A synthetic 8-bit unsigned stereo WAV at 22050 Hz exercises downmix +
// resample + the 8-bit decode path.
func TestReadWAV8BitStereoResample(t *testing.T) {
	const srcRate = 22050
	const frames = 2205 // 0.1 s
	data := make([]byte, frames*2)
	for f := 0; f < frames; f++ {
		// L = +1.0 (255), R = -1.0 (1) -> mono average ~0.
		data[f*2] = 255
		data[f*2+1] = 1
	}
	path := filepath.Join(t.TempDir(), "s8.wav")
	if err := os.WriteFile(path, build8BitStereoWAV(srcRate, data), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _, err := ReadWAVMono44k(path)
	if err != nil {
		t.Fatal(err)
	}
	// 22050 -> 44100 doubles the length (approx).
	if out == nil || len(out) < frames {
		t.Fatalf("expected upsampled output, got %d samples", len(out))
	}
	// Average of +0.99 and -0.99 should be near zero.
	for i, s := range out {
		if math.Abs(float64(s)) > 0.05 {
			t.Fatalf("downmix not centered at sample %d: %g", i, s)
		}
	}
}

func build8BitStereoWAV(rate int, data []byte) []byte {
	put16 := func(b []byte, v uint16) { b[0] = byte(v); b[1] = byte(v >> 8) }
	put32 := func(b []byte, v uint32) {
		b[0] = byte(v)
		b[1] = byte(v >> 8)
		b[2] = byte(v >> 16)
		b[3] = byte(v >> 24)
	}
	const channels = 2
	const bits = 8
	blockAlign := channels * bits / 8
	hdr := make([]byte, 44)
	copy(hdr[0:], "RIFF")
	put32(hdr[4:], uint32(36+len(data)))
	copy(hdr[8:], "WAVE")
	copy(hdr[12:], "fmt ")
	put32(hdr[16:], 16)
	put16(hdr[20:], wavFmtPCM)
	put16(hdr[22:], channels)
	put32(hdr[24:], uint32(rate))
	put32(hdr[28:], uint32(rate*blockAlign))
	put16(hdr[32:], uint16(blockAlign))
	put16(hdr[34:], bits)
	copy(hdr[36:], "data")
	put32(hdr[40:], uint32(len(data)))
	return append(hdr, data...)
}
