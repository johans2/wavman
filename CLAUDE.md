# wavman

NES-style WAV sound generator for a platform game with NES aesthetic.
Inspired by bfxr.net but native, with live waveform/envelope visualization,
mutation history, and an audio model oriented around the NES 2A03 APU palette.

## Goal

The game this tool feeds has a NES aesthetic, so audio choices target NES
authenticity (Ricoh 2A03):
- 2 pulse channels (square + duty cycle), 1 triangle, 1 noise (LFSR)
- 4-bit effective DAC depth
- Low-pass around ~14 kHz effective bandwidth

Prefer NES-authentic defaults over generic chiptune ones. Examples:
- 4-bit quantization for the 8-bit toggle (matches NES DAC).
- Short-mode LFSR (bit0 XOR bit6) for metallic noise.
- Square duty cycles (12.5/25/50/75 are the NES set, but the slider is continuous).
- Vibrato, arpeggio — NES music idioms.
- Skip suggestions that wouldn't fit the era (reverb, chorus, sidechain).

## Hard constraint: NO CGO

This dev machine's external linker (TDM-GCC) produces PE binaries that fail
to launch with "%1 is not a valid Win32 application" / "This app can't run
on your PC". Pure-Go binaries work; any CGO binary does not. So everything
in wavman is pure-Go:

- GUI: `gioui.org` — D3D11 via syscalls on Windows, no CGO
- Audio: `github.com/ebitengine/oto/v3` — winmm via syscalls, no CGO
- File dialog: `github.com/ncruces/zenity` — Windows Shell API via syscalls, no CGO

**Do NOT add CGO-dependent libraries** (fyne, glfw, libui, beep+portaudio,
sqweek/dialog, etc.) — they will build fine but won't launch on this machine.

## Build & run

    go build -ldflags="-H=windowsgui" -o wavman.exe .

The `-H=windowsgui` flag is required: without it you get a stray console
window alongside the GUI. Module is `wavman`, Go 1.25+.

## Files

- `synth.go` — `Params` struct, `Render()`, `Envelope()`, `PeakRMS()`. The
  full synthesis pipeline (see below).
- `wav.go` — 16-bit PCM mono WAV writer, used both for file export and for
  building the byte buffer the oto player consumes.
- `presets.go` — bfxr-style preset templates (Pickup, Laser, Explosion, Hit,
  Jump, Blip). Each returns a randomized `Params`.
- `player.go` — oto/v3 singleton wrapper. One process-wide audio context,
  `Play(samples)` cancels any current playback and starts a new one. A
  ticker goroutine fires the playhead callback at ~60Hz while audio runs.
- `drawing.go` — Gio custom widgets. `WaveformView` draws a filled polygon
  between per-column sample min/max plus a playhead overlay. `EnvelopeView`
  draws the live ADSR curve (fill below + stroked top line).
- `history.go` — `History` type: capped list of `Params` with a current
  index. `Push` truncates redo-history and FIFO-drops at `historyMax` (10).
- `main.go` — Gio app entry, UI state struct, layout, event/click
  handling, all slider/button/checkbox plumbing. Long-ish; ~600 lines.

## Architecture

### One-way data flow

The UI is the source of truth for sound parameters:

    widgets (sliders, checkboxes, waveform/preset buttons)
        ↓ readParams()
    Params
        ↓ Render()
    []float32 samples
        ↓
    WaveformView (draw)   Player.Play (audio out)   WriteWAV (export)

Every frame the layout loop reads current widget state into `Params`. If it
differs from the previous `ui.params` (`newP != ui.params`), `Render()`
refreshes the samples, which updates the waveform view, the peak/RMS
readout, and triggers playback when auto-play is on (or immediately for
discrete button clicks like waveform/preset).

### Modules pattern

Optional features ("modules") all follow a consistent pattern:
- An enable checkbox (`widget.Bool` in the UI struct).
- Zero or more sliders that are **hidden when the checkbox is off** (the
  layout dynamically appends children to the flex).
- `Params` has a `XEnabled bool` plus the values for X.
- `Render` checks `p.XEnabled` before applying the effect.

Current modules:
- **8-bit**, **Reverse**, **Auto-play** — top-bar checkboxes, single toggles.
- **Duty cycle**, **Vibrato**, **Arpeggio** — Modulation card (always visible).
- **Noise Pitch**, **Metallic**, **Filter** — Noise card, visible only when
  the Noise waveform is selected.

Adding a new module: add fields to `Params`, branch in `Render`, add
`widget.Bool` + `*sliderState` fields on `UI`, initialize in `newUI`, wire
into `readParams` + `syncSlidersFromParams`, append a section in the
relevant card.

### Mutation history

`history.go` tracks discrete checkpoints, NOT every slider tweak:
- Mutate (the ► arrow when at end of history)
- Preset clicks
- Waveform button clicks

Slider tweaks update the *current* entry in place via `SetCurrent` — they
don't create new entries but they are preserved when navigating away and
back. Capped at 10 entries; pushing the 11th drops the oldest (FIFO).

The ► button does double duty: at end of history it generates a new
mutation (label shows "► Mutate"); otherwise it navigates re-do (label
shows just "►"). ◄ steps back and is dimmed at history start.

### Slider helper (`sliderState`)

Wraps `widget.Float` with min/max/format-string-fn. `Get()` returns the
value in the real range; `Set()` reverse-maps to the 0..1 widget range.
Each row renders as `[label] [slider] [formatted value]` via a horizontal
flex.

### Auto-play debounce

When the user drags a slider with auto-play on, we don't want to retrigger
playback every frame. The layout function schedules a future frame via
`gtx.Execute(op.InvalidateCmd{At: deadline})` (180ms after the last param
change). When that frame fires and nothing has changed since, play once.
Discrete clicks (waveform/preset/mutate) bypass the debounce via the
`immediatePlay` flag and play instantly.

## Audio pipeline (`Render`)

State persisted across the per-sample loop (LFSR, sample-and-hold
counter, filter accumulator) is declared above the loop.

For each output sample `i`:
1. `t = i / SampleRate`, `progress = t / Duration`
2. `freq = StartFreq + (EndFreq - StartFreq) * progress`
3. If Vibrato enabled: sine LFO modulates freq by `VibratoDepth` cents at
   `VibratoRate` Hz. Conversion: `freq *= 2^(cents/1200)`.
4. If Arpeggio enabled: cycles base / +Semi1 / +Semi2 in semitones at
   `ArpRate` steps/sec. Conversion: `freq *= 2^(semis/12)`.
5. Waveform branch:
   - **Sine / Saw / Triangle**: phase-based math, phase advances by
     `2π * freq / SampleRate` per sample.
   - **Square**: same, with duty threshold (0.5 default, `Duty` if
     `DutyEnabled`).
   - **Noise**: see below.
6. Multiply by `Envelope(t, Duration, A, D, S, R)`. Envelope auto-scales
   A+D+R if they exceed Duration.
7. Multiply by `Volume`.

After the loop:
8. **8-bit**: 4-bit quantize each sample (16 levels, step 0.125). No
   decimation — earlier 3× decimation caused aliasing harshness; pure
   bitcrush is enough for the chiptune feel.
9. **Reverse**: in-place buffer reverse.

### Noise pipeline (step 5 branch)

- **Metallic on**: 15-bit LFSR with `bit0 XOR bit6` feedback (NES short
  mode, period 93). The LFSR is driven by a fractional phase accumulator
  at `93 × MetallicPitch` Hz, so the output fundamental equals the
  `MetallicPitch` slider value (default 3800 Hz). The Noise Pitch
  sample-and-hold is ignored in this mode — Metallic has its own pitch
  control and is its own self-contained noise source.
- **Metallic off**: uniform random with optional Pitch sample-and-hold:
  hold `N = SampleRate / NoisePitch` output samples per random value
  (effective rate = `NoisePitch` Hz).

After either branch, optional one-pole IIR low-pass:
`a = dt / (RC + dt)` where `RC = 1 / (2π * cutoff)`, then
`y += a * (x - y)`.

## UI layout

    +----------------------------------------------------------+
    | Wave: [Sine][Square][Saw][Triangle][Noise]   [8-bit][Reverse][Auto-play] [Play] [◄][► Mutate] [Export]
    +-------------------+-------------------------------------+
    |                   |                                     |
    |   Presets         |   Waveform                          |
    |   [6 buttons]     |   <waveform display, playhead>      |
    |                   |                                     |
    |   Parameters      |   ADSR envelope                     |
    |   [8 sliders]     |   <envelope display>                |
    |                   |                                     |
    |   Modulation      |   Peak / RMS / samples              |
    |   [3 modules]     |                                     |
    |                   |                                     |
    |   (Noise card,    |                                     |
    |    if Noise)      |                                     |
    +-------------------+-------------------------------------+
    | (status line for save messages)                          |
    +----------------------------------------------------------+

Window default 1100×820. The left panel grows when modules are enabled
or when the Noise waveform is selected — bump the window height if you
add another full module.

## Common pitfalls

- **Don't add CGO libs.** See top of this doc.
- **Always build with `-H=windowsgui`** or you get a stray console window.
- The `Render` loop is hot; persistent state (LFSR, hold counters, filter
  history) is declared above the loop and mutated in place per sample. If
  you add new noise/modulation state, follow that pattern.
- The player allows only one sound at a time; a new `Play()` cancels the
  old one. The oto context is a process-wide singleton created once via
  `GetPlayer`.
- When syncing `Params` back to widgets (presets, history navigation),
  `slider.Set()` goes through a float32 roundtrip that may cause the
  change-detection branch to fire on the next frame with slightly
  different values. This is benign — `history.SetCurrent` just updates
  the current entry with the readback values.
- `params != newP` is a struct compare — make sure new `Params` fields
  are comparable (no slices/maps).

## Commit style

Past commits are terse, lowercase, present-tense imperative. One-line
subject summarizing the user-visible change, optional bullet body for
non-trivial details. Bodies include
`Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>`.
