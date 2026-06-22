package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
)

// WAV format codes (wFormatTag).
const (
	wavFmtPCM        = 0x0001
	wavFmtFloat      = 0x0003
	wavFmtExtensible = 0xFFFE
)

// maxImportSamples caps an imported clip's length (in 44.1k mono samples) so a
// stray multi-minute file can't balloon memory or the waveform view. ~60 s.
const maxImportSamples = 60 * sampleRate

// ReadWAVMono44k loads a WAV file and returns it as mono float32 samples in
// [-1,1] at the app's 44100 Hz pipeline rate. Stereo/multichannel is downmixed
// by averaging; non-44.1k rates are linearly resampled. Supports PCM 8/16/24/32
// and IEEE float 32/64, including WAVE_FORMAT_EXTENSIBLE. The returned truncated
// flag is true if the clip was longer than maxImportSamples and was cut.
func ReadWAVMono44k(path string) (samples []float32, truncated bool, err error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, false, err
	}
	if len(raw) < 12 || string(raw[0:4]) != "RIFF" || string(raw[8:12]) != "WAVE" {
		return nil, false, fmt.Errorf("not a RIFF/WAVE file")
	}

	var (
		format    int
		channels  int
		srcRate   int
		bits      int
		dataChunk []byte
		haveFmt   bool
		haveData  bool
	)

	// Walk chunks: 4-byte id, 4-byte LE size, payload (padded to even length).
	pos := 12
	for pos+8 <= len(raw) {
		id := string(raw[pos : pos+4])
		size := int(binary.LittleEndian.Uint32(raw[pos+4 : pos+8]))
		body := pos + 8
		if body+size > len(raw) {
			size = len(raw) - body // tolerate a truncated/under-reported final chunk
		}
		switch id {
		case "fmt ":
			if size < 16 {
				return nil, false, fmt.Errorf("fmt chunk too small")
			}
			format = int(binary.LittleEndian.Uint16(raw[body : body+2]))
			channels = int(binary.LittleEndian.Uint16(raw[body+2 : body+4]))
			srcRate = int(binary.LittleEndian.Uint32(raw[body+4 : body+8]))
			bits = int(binary.LittleEndian.Uint16(raw[body+14 : body+16]))
			if format == wavFmtExtensible && size >= 40 {
				// Real format lives in the first 2 bytes of the SubFormat GUID.
				format = int(binary.LittleEndian.Uint16(raw[body+24 : body+26]))
			}
			haveFmt = true
		case "data":
			dataChunk = raw[body : body+size]
			haveData = true
		}
		pos = body + size
		if size%2 == 1 {
			pos++ // chunks are word-aligned
		}
	}

	if !haveFmt || !haveData {
		return nil, false, fmt.Errorf("missing fmt or data chunk")
	}
	if channels < 1 {
		return nil, false, fmt.Errorf("invalid channel count %d", channels)
	}
	if srcRate < 1 {
		return nil, false, fmt.Errorf("invalid sample rate %d", srcRate)
	}

	mono, err := decodeFramesMono(dataChunk, format, bits, channels)
	if err != nil {
		return nil, false, err
	}
	if len(mono) == 0 {
		return nil, false, fmt.Errorf("no audio samples in file")
	}

	out := resampleLinear(mono, srcRate, sampleRate)
	if len(out) > maxImportSamples {
		out = out[:maxImportSamples]
		truncated = true
	}
	return out, truncated, nil
}

// decodeFramesMono decodes interleaved PCM/float frames into mono float32 by
// averaging channels.
func decodeFramesMono(data []byte, format, bits, channels int) ([]float32, error) {
	bytesPerSample := bits / 8
	if bytesPerSample < 1 {
		return nil, fmt.Errorf("unsupported bit depth %d", bits)
	}
	frameSize := bytesPerSample * channels
	if frameSize == 0 {
		return nil, fmt.Errorf("invalid frame size")
	}
	nFrames := len(data) / frameSize

	decode := sampleDecoder(format, bits)
	if decode == nil {
		return nil, fmt.Errorf("unsupported WAV format (tag %d, %d-bit)", format, bits)
	}

	out := make([]float32, nFrames)
	for f := 0; f < nFrames; f++ {
		base := f * frameSize
		var sum float64
		for c := 0; c < channels; c++ {
			sum += decode(data[base+c*bytesPerSample:])
		}
		out[f] = float32(sum / float64(channels))
	}
	return out, nil
}

// sampleDecoder returns a function that reads one sample (from the start of the
// given slice) as a float64 in [-1,1], or nil if the format is unsupported.
func sampleDecoder(format, bits int) func([]byte) float64 {
	switch format {
	case wavFmtPCM:
		switch bits {
		case 8: // unsigned, midpoint 128
			return func(b []byte) float64 { return (float64(b[0]) - 128) / 128 }
		case 16:
			return func(b []byte) float64 {
				return float64(int16(binary.LittleEndian.Uint16(b))) / 32768
			}
		case 24:
			return func(b []byte) float64 {
				v := int32(b[0]) | int32(b[1])<<8 | int32(b[2])<<16
				if v&0x800000 != 0 {
					v |= ^0xFFFFFF // sign-extend
				}
				return float64(v) / 8388608
			}
		case 32:
			return func(b []byte) float64 {
				return float64(int32(binary.LittleEndian.Uint32(b))) / 2147483648
			}
		}
	case wavFmtFloat:
		switch bits {
		case 32:
			return func(b []byte) float64 {
				return float64(math.Float32frombits(binary.LittleEndian.Uint32(b)))
			}
		case 64:
			return func(b []byte) float64 {
				return math.Float64frombits(binary.LittleEndian.Uint64(b))
			}
		}
	}
	return nil
}

// resampleLinear converts mono samples from srcRate to dstRate via linear
// interpolation. A no-op when the rates already match.
func resampleLinear(in []float32, srcRate, dstRate int) []float32 {
	if srcRate == dstRate || len(in) == 0 {
		return in
	}
	ratio := float64(srcRate) / float64(dstRate)
	outLen := int(float64(len(in)) / ratio)
	if outLen < 1 {
		outLen = 1
	}
	out := make([]float32, outLen)
	for j := range out {
		pos := float64(j) * ratio
		i := int(pos)
		frac := float32(pos - float64(i))
		if i+1 < len(in) {
			out[j] = in[i]*(1-frac) + in[i+1]*frac
		} else {
			out[j] = in[i]
		}
	}
	return out
}
