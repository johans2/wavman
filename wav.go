package main

import (
	"bytes"
	"encoding/binary"
	"io"
	"os"
)

// SamplesToPCM16 converts float samples in [-1,1] to little-endian 16-bit PCM bytes.
func SamplesToPCM16(samples []float32) []byte {
	buf := make([]byte, len(samples)*2)
	for i, s := range samples {
		if s > 1 {
			s = 1
		} else if s < -1 {
			s = -1
		}
		v := int16(s * 32767)
		buf[2*i] = byte(v)
		buf[2*i+1] = byte(v >> 8)
	}
	return buf
}

func WriteWAV(filename string, samples []float32, sampleRate int) error {
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()
	return writeWAVTo(f, samples, sampleRate)
}

func writeWAVTo(w io.Writer, samples []float32, sampleRate int) error {
	pcm := SamplesToPCM16(samples)
	dataSize := uint32(len(pcm))

	var hdr bytes.Buffer
	hdr.WriteString("RIFF")
	binary.Write(&hdr, binary.LittleEndian, uint32(36+dataSize))
	hdr.WriteString("WAVE")

	hdr.WriteString("fmt ")
	binary.Write(&hdr, binary.LittleEndian, uint32(16))
	binary.Write(&hdr, binary.LittleEndian, uint16(1))                // PCM
	binary.Write(&hdr, binary.LittleEndian, uint16(1))                // mono
	binary.Write(&hdr, binary.LittleEndian, uint32(sampleRate))       // sample rate
	binary.Write(&hdr, binary.LittleEndian, uint32(sampleRate*2))     // byte rate
	binary.Write(&hdr, binary.LittleEndian, uint16(2))                // block align
	binary.Write(&hdr, binary.LittleEndian, uint16(16))               // bits/sample

	hdr.WriteString("data")
	binary.Write(&hdr, binary.LittleEndian, dataSize)

	if _, err := w.Write(hdr.Bytes()); err != nil {
		return err
	}
	_, err := w.Write(pcm)
	return err
}
