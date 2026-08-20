package stream

import "sync"

// Controls carries live pause/resume commands into a running Play call. A nil
// *Controls never pauses, so callers without transport controls can pass nil.
type Controls struct {
	mu     sync.Mutex
	paused bool
}

func NewControls() *Controls { return &Controls{} }

// Pause freezes playback at the current position. Returns false when already
// paused.
func (c *Controls) Pause() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.paused {
		return false
	}
	c.paused = true
	return true
}

// Resume continues playback after Pause. Returns false when not paused.
func (c *Controls) Resume() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.paused {
		return false
	}
	c.paused = false
	return true
}

func (c *Controls) Paused() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.paused
}
