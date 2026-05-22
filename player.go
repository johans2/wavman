package main

import (
	"bytes"
	"fmt"
	"sync"
	"time"

	"github.com/ebitengine/oto/v3"
)

type Player struct {
	ctx        *oto.Context
	sampleRate int

	mu         sync.Mutex
	cur        *oto.Player
	stopTicker chan struct{}
	onPlayhead func(float64)
}

var (
	playerOnce sync.Once
	playerInst *Player
	playerErr  error
)

// GetPlayer returns the process-wide oto context as a Player.
// oto allows only one context per process so this is a singleton.
func GetPlayer(sampleRate int) (*Player, error) {
	playerOnce.Do(func() {
		opt := &oto.NewContextOptions{
			SampleRate:   sampleRate,
			ChannelCount: 1,
			Format:       oto.FormatSignedInt16LE,
		}
		ctx, ready, err := oto.NewContext(opt)
		if err != nil {
			playerErr = fmt.Errorf("oto.NewContext: %w", err)
			return
		}
		<-ready
		playerInst = &Player{ctx: ctx, sampleRate: sampleRate}
	})
	return playerInst, playerErr
}

// SetPlayheadCallback registers a function called with playback progress (0..1)
// at ~60Hz from a background goroutine. Caller must be thread-safe.
func (p *Player) SetPlayheadCallback(fn func(float64)) {
	p.mu.Lock()
	p.onPlayhead = fn
	p.mu.Unlock()
}

func (p *Player) Play(samples []float32) {
	if len(samples) == 0 {
		return
	}
	p.mu.Lock()
	if p.cur != nil {
		_ = p.cur.Close()
		p.cur = nil
	}
	if p.stopTicker != nil {
		close(p.stopTicker)
		p.stopTicker = nil
	}

	pcm := SamplesToPCM16(samples)
	player := p.ctx.NewPlayer(bytes.NewReader(pcm))
	p.cur = player
	startedAt := time.Now()
	duration := time.Duration(float64(len(samples)) / float64(p.sampleRate) * float64(time.Second))
	stop := make(chan struct{})
	p.stopTicker = stop
	cb := p.onPlayhead
	p.mu.Unlock()

	player.Play()

	if cb != nil {
		go func() {
			ticker := time.NewTicker(16 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-stop:
					cb(0)
					return
				case <-ticker.C:
					elapsed := time.Since(startedAt)
					if elapsed >= duration {
						cb(1)
						time.Sleep(60 * time.Millisecond)
						cb(0)
						return
					}
					cb(float64(elapsed) / float64(duration))
				}
			}
		}()
	}
}
