package main

import (
	"image"
	"image/color"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
)

var (
	bgColor       = color.NRGBA{R: 18, G: 20, B: 30, A: 255}
	gridColor     = color.NRGBA{R: 45, G: 48, B: 65, A: 255}
	centerColor   = color.NRGBA{R: 70, G: 75, B: 100, A: 255}
	waveColor     = color.NRGBA{R: 120, G: 230, B: 150, A: 255}
	envLineColor  = color.NRGBA{R: 100, G: 180, B: 250, A: 255}
	envFillColor  = color.NRGBA{R: 45, G: 90, B: 140, A: 110}
	playheadColor = color.NRGBA{R: 255, G: 200, B: 90, A: 255}
)

func paintFill(gtx layout.Context, r image.Rectangle, c color.NRGBA) {
	defer clip.Rect(r).Push(gtx.Ops).Pop()
	paint.ColorOp{Color: c}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
}

type WaveformView struct {
	Samples  []float32
	Playhead float64
}

func (wf WaveformView) Layout(gtx layout.Context) layout.Dimensions {
	size := gtx.Constraints.Max
	width := size.X
	height := size.Y
	if width <= 0 || height <= 0 {
		return layout.Dimensions{Size: size}
	}

	paintFill(gtx, image.Rect(0, 0, width, height), bgColor)
	paintFill(gtx, image.Rect(0, height/4, width, height/4+1), gridColor)
	paintFill(gtx, image.Rect(0, height*3/4, width, height*3/4+1), gridColor)
	paintFill(gtx, image.Rect(0, height/2, width, height/2+1), centerColor)

	n := len(wf.Samples)
	if n > 0 {
		cy := float32(height) / 2
		amp := float32(height)/2 - 2

		var path clip.Path
		path.Begin(gtx.Ops)
		// top trace, left to right
		for x := 0; x < width; x++ {
			startIdx := x * n / width
			endIdx := (x + 1) * n / width
			if endIdx <= startIdx {
				endIdx = startIdx + 1
			}
			if endIdx > n {
				endIdx = n
			}
			maxV := wf.Samples[startIdx]
			for i := startIdx + 1; i < endIdx; i++ {
				if wf.Samples[i] > maxV {
					maxV = wf.Samples[i]
				}
			}
			y := cy - maxV*amp
			if x == 0 {
				path.MoveTo(f32.Pt(0, y))
			} else {
				path.LineTo(f32.Pt(float32(x), y))
			}
		}
		// bottom trace, right to left
		for x := width - 1; x >= 0; x-- {
			startIdx := x * n / width
			endIdx := (x + 1) * n / width
			if endIdx <= startIdx {
				endIdx = startIdx + 1
			}
			if endIdx > n {
				endIdx = n
			}
			minV := wf.Samples[startIdx]
			for i := startIdx + 1; i < endIdx; i++ {
				if wf.Samples[i] < minV {
					minV = wf.Samples[i]
				}
			}
			y := cy - minV*amp
			path.LineTo(f32.Pt(float32(x), y))
		}
		path.Close()

		outline := clip.Outline{Path: path.End()}
		cl := outline.Op().Push(gtx.Ops)
		paint.ColorOp{Color: waveColor}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		cl.Pop()
	}

	if wf.Playhead > 0 && wf.Playhead <= 1 {
		px := int(wf.Playhead * float64(width))
		if px >= width {
			px = width - 1
		}
		if px >= 0 {
			paintFill(gtx, image.Rect(px, 0, px+1, height), playheadColor)
		}
	}

	return layout.Dimensions{Size: size}
}

type EnvelopeView struct {
	Params Params
}

func (ev EnvelopeView) Layout(gtx layout.Context) layout.Dimensions {
	size := gtx.Constraints.Max
	width := size.X
	height := size.Y
	if width < 2 || height < 4 {
		return layout.Dimensions{Size: size}
	}

	paintFill(gtx, image.Rect(0, 0, width, height), bgColor)
	paintFill(gtx, image.Rect(0, height-1, width, height), gridColor)

	d := ev.Params.Duration
	if d <= 0 {
		return layout.Dimensions{Size: size}
	}

	usable := float32(height - 2)
	envY := make([]float32, width)
	for x := 0; x < width; x++ {
		t := float64(x) / float64(width-1) * d
		e := Envelope(t, d, ev.Params.Attack, ev.Params.Decay, ev.Params.Sustain, ev.Params.Release)
		envY[x] = float32(height-1) - float32(e)*usable
	}

	// Filled area under the curve.
	var fillPath clip.Path
	fillPath.Begin(gtx.Ops)
	fillPath.MoveTo(f32.Pt(0, float32(height-1)))
	for x := 0; x < width; x++ {
		fillPath.LineTo(f32.Pt(float32(x), envY[x]))
	}
	fillPath.LineTo(f32.Pt(float32(width-1), float32(height-1)))
	fillPath.Close()
	fillOutline := clip.Outline{Path: fillPath.End()}
	cl := fillOutline.Op().Push(gtx.Ops)
	paint.ColorOp{Color: envFillColor}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	cl.Pop()

	// Stroked top line.
	var linePath clip.Path
	linePath.Begin(gtx.Ops)
	linePath.MoveTo(f32.Pt(0, envY[0]))
	for x := 1; x < width; x++ {
		linePath.LineTo(f32.Pt(float32(x), envY[x]))
	}
	stroke := clip.Stroke{Path: linePath.End(), Width: 1.5}
	cl2 := stroke.Op().Push(gtx.Ops)
	paint.ColorOp{Color: envLineColor}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	cl2.Pop()

	return layout.Dimensions{Size: size}
}
