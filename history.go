package main

const historyMax = 10

type History struct {
	entries []Params
	index   int
}

func NewHistory(initial Params) *History {
	return &History{entries: []Params{initial}, index: 0}
}

func (h *History) Current() Params { return h.entries[h.index] }

func (h *History) CanBack() bool { return h.index > 0 }

func (h *History) AtEnd() bool { return h.index >= len(h.entries)-1 }

// SetCurrent overwrites the current entry; used so slider tweaks
// are remembered when the user navigates away and returns.
func (h *History) SetCurrent(p Params) { h.entries[h.index] = p }

// Push appends p after the current index, discards any redo-history
// after it, and trims to historyMax by dropping the oldest entries.
func (h *History) Push(p Params) {
	h.entries = append(h.entries[:h.index+1], p)
	h.index = len(h.entries) - 1
	if len(h.entries) > historyMax {
		drop := len(h.entries) - historyMax
		h.entries = h.entries[drop:]
		h.index -= drop
		if h.index < 0 {
			h.index = 0
		}
	}
}

func (h *History) Back() Params {
	if h.index > 0 {
		h.index--
	}
	return h.entries[h.index]
}

func (h *History) Forward() Params {
	if h.index < len(h.entries)-1 {
		h.index++
	}
	return h.entries[h.index]
}
