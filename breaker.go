package breaker

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type State string

const (
	Closed   State = "closed"
	HalfOpen State = "half-open"
	Open     State = "open"

	_windowRoll      = 300
	_windowFrame     = 15
	_halfOpenTimeout = 30
)

var (
	ErrNewCircuitBreaker = errors.New("failed to create circuit breaker")
	ErrOpenCircuit       = errors.New("circuit open")
)

type (
	circuitCall         func() error
	canTrip             func(summary Counts) bool
	fromHalfOpenToState func(summary Counts) State
)

type CircuitBreaker struct {
	state             *state
	onHalfOpenTimeout atomic.Bool

	canTrip             canTrip
	fromHalfOpenToState fromHalfOpenToState

	cfg configuration

	rollingWindow *rollingWindow
	summary       *summary
}

type Counts struct {
	Total   uint64
	Fail    uint64
	Success uint64
}

type rollingWindow struct {
	window []Counts

	mu sync.RWMutex
}

type summary struct {
	counts Counts

	mu sync.RWMutex
}

type state struct {
	s State

	mu sync.RWMutex
}

type configuration struct {
	windowRoll      time.Duration
	windowFrame     time.Duration
	halfOpenTimeout time.Duration
}

func New(opts ...option) (cb *CircuitBreaker, cancel func(), err error) {
	cbOpts := &optionsConfiguration{
		windowFrame:       _windowFrame,
		windowRoll:        _windowRoll,
		halfOpenThreshold: _halfOpenTimeout,

		canTrip:             defaultCanTrip,
		fromHalfOpenToState: defaultFromHalfOpenToState,
	}

	for _, opt := range opts {
		if err = opt(cbOpts); err != nil {
			return cb, cancel, fmt.Errorf("%w: %s", ErrNewCircuitBreaker, err)
		}
	}

	if cbOpts.windowFrame > cbOpts.windowRoll {
		return cb, cancel, fmt.Errorf("%w: invalid window threshold", ErrNewCircuitBreaker)
	}

	frames := cbOpts.windowRoll / cbOpts.windowFrame
	cb = &CircuitBreaker{
		cfg: configuration{
			windowRoll:      (time.Second * time.Duration(cbOpts.windowRoll)),
			windowFrame:     (time.Second * time.Duration(cbOpts.windowFrame)),
			halfOpenTimeout: (time.Second * time.Duration(cbOpts.halfOpenThreshold)),
		},
		canTrip:             cbOpts.canTrip,
		fromHalfOpenToState: cbOpts.fromHalfOpenToState,

		state: &state{
			s: Closed,
		},
		rollingWindow: &rollingWindow{
			window: make([]Counts, frames, (frames + 2)),
		},
		summary: &summary{
			counts: Counts{},
		},
	}

	cancelCh := make(chan struct{})
	cancel = cancelFunc(cancelCh)
	go cb.renewFrame(cancelCh)

	return cb, cancel, nil
}

func (c *CircuitBreaker) renewFrame(cancel <-chan struct{}) {
	for {
		select {
		case <-time.After(c.cfg.windowFrame):
			if c.stateCopy() != Closed {
				return
			}
			c.moveWindow()
		case <-cancel:
			return
		}
	}
}

func (c *CircuitBreaker) Execute(fn circuitCall) error {
	defer c.afterExecute()

	if err := c.canExecute(); err != nil {
		return err
	}

	defer func() {
		if r := recover(); r != nil {
			c.incrFail()
			panic(r)
		}
	}()

	if err := fn(); err != nil {
		c.incrFail()
		return err
	}

	c.incrSuccess()
	return nil
}

func (c *CircuitBreaker) afterExecute() {
	c.state.mu.Lock()
	defer c.state.mu.Unlock()

	switch c.state.s {
	case Closed:
		if c.canTrip(c.summaryCopy()) {
			c.state.s = Open
			go c.waitHalfOpen()
		}

	case HalfOpen:
		switch c.fromHalfOpenToState(c.currentFrameCopy()) {
		case Open:
			if c.onHalfOpenTimeout.Load() {
				return
			}
			go c.waitHalfOpen()

			c.state.s = Open
			c.popWindow()

		case Closed:
			c.state.s = Closed
			c.aggregateHalfOpenFrame()
		}
	}
}

func (c *CircuitBreaker) waitHalfOpen() {
	c.onHalfOpenTimeout.Store(true)
	defer c.onHalfOpenTimeout.Store(false)

	<-time.After(c.cfg.halfOpenTimeout)

	c.state.mu.Lock()
	defer c.state.mu.Unlock()
	c.state.s = HalfOpen
	c.addFrame()
}

func (c *CircuitBreaker) canExecute() error {
	c.state.mu.RLock()
	defer c.state.mu.RUnlock()

	if c.state.s == Open {
		return ErrOpenCircuit
	}

	return nil
}

func (c *CircuitBreaker) moveWindow() {
	c.decrSummary(c.unshiftFrame())
	c.addFrame()
}

func (c *CircuitBreaker) aggregateHalfOpenFrame() {
	halfOpenFrame := c.popFrame()
	c.rollingWindow.mu.Lock()
	defer c.rollingWindow.mu.Unlock()
	c.rollingWindow.window[(len(c.rollingWindow.window) - 1)].Total += halfOpenFrame.Total
	c.rollingWindow.window[(len(c.rollingWindow.window) - 1)].Success += halfOpenFrame.Success
	c.rollingWindow.window[(len(c.rollingWindow.window) - 1)].Fail += halfOpenFrame.Fail
}

// unshiftFrame Removes the first frame from the rolling window.
func (c *CircuitBreaker) unshiftFrame() Counts {
	c.rollingWindow.mu.Lock()
	defer c.rollingWindow.mu.Unlock()
	defer func() {
		c.rollingWindow.window = append(make([]Counts, 0, cap(c.rollingWindow.window)), c.rollingWindow.window[1:]...)
	}()

	return c.rollingWindow.window[0]
}

func (c *CircuitBreaker) addFrame() {
	c.rollingWindow.mu.Lock()
	defer c.rollingWindow.mu.Unlock()
	c.rollingWindow.window = append(c.rollingWindow.window, Counts{})
}

func (c *CircuitBreaker) popWindow() {
	c.decrSummary(c.popFrame())
}

// popFrame Removes the last frame from the rolling window.
func (c *CircuitBreaker) popFrame() Counts {
	c.rollingWindow.mu.Lock()
	defer c.rollingWindow.mu.Unlock()
	defer func() {
		c.rollingWindow.window = c.rollingWindow.window[:(len(c.rollingWindow.window) - 1)]
	}()

	return c.rollingWindow.window[(len(c.rollingWindow.window) - 1)]
}

func (c *CircuitBreaker) incrSuccess() {
	c.rollingWindow.mu.Lock()
	c.summary.mu.Lock()
	defer c.rollingWindow.mu.Unlock()
	defer c.summary.mu.Unlock()

	c.rollingWindow.window[(len(c.rollingWindow.window) - 1)].Total += 1
	c.rollingWindow.window[(len(c.rollingWindow.window) - 1)].Success += 1
	c.summary.counts.Total += 1
	c.summary.counts.Success += 1
}

func (c *CircuitBreaker) incrFail() {
	c.rollingWindow.mu.Lock()
	c.summary.mu.Lock()
	defer c.rollingWindow.mu.Unlock()
	defer c.summary.mu.Unlock()

	c.rollingWindow.window[(len(c.rollingWindow.window) - 1)].Total += 1
	c.rollingWindow.window[(len(c.rollingWindow.window) - 1)].Fail += 1
	c.summary.counts.Fail += 1
	c.summary.counts.Total += 1
}

func (c *CircuitBreaker) decrSummary(decr Counts) {
	c.summary.mu.Lock()
	defer c.summary.mu.Unlock()

	c.summary.counts.Fail -= decr.Fail
	c.summary.counts.Success -= decr.Success
	c.summary.counts.Total -= decr.Total
}

func (c *CircuitBreaker) stateCopy() State {
	c.state.mu.RLock()
	defer c.state.mu.RUnlock()
	return c.state.s
}

func (c *CircuitBreaker) summaryCopy() Counts {
	c.summary.mu.RLock()
	defer c.summary.mu.RUnlock()
	return c.summary.counts
}

func (c *CircuitBreaker) currentFrameCopy() Counts {
	c.rollingWindow.mu.RLock()
	defer c.rollingWindow.mu.RUnlock()
	return c.rollingWindow.window[(len(c.rollingWindow.window) - 1)]
}

func (c *CircuitBreaker) windowCopy() []Counts {
	c.rollingWindow.mu.RLock()
	defer c.rollingWindow.mu.RUnlock()
	cw := make([]Counts, len(c.rollingWindow.window), cap(c.rollingWindow.window))
	copy(cw, c.rollingWindow.window)
	return cw
}
