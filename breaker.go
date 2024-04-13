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
	ErrHalfOpenCircuit   = errors.New("circuit half open")
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

	mu sync.Mutex
}

type summary struct {
	counts Counts

	mu sync.Mutex
}

type state struct {
	s State

	mu sync.Mutex
}

type configuration struct {
	windowRoll      time.Duration
	windowFrame     time.Duration
	halfOpenTimeout time.Duration
}

func New(opts ...option) (cb *CircuitBreaker, cancel func(), err error) {
	controller := &stateControler{halfOpenTimeout: time.Second * _halfOpenTimeout}
	cbOpts := &optionsConfiguration{
		windowFrame:       _windowFrame,
		windowRoll:        _windowRoll,
		halfOpenThreshold: _halfOpenTimeout,

		canTrip:             controller.defaultCanTrip,
		fromHalfOpenToState: controller.defaultFromHalfOpenToState,
	}

	for _, opt := range opts {
		if err = opt(cbOpts); err != nil {
			return cb, cancel, fmt.Errorf("%w: %s", ErrNewCircuitBreaker, err)
		}
	}

	if cbOpts.windowFrame > cbOpts.windowRoll {
		return cb, cancel, fmt.Errorf("%w: frame threshold can't be minor than window roll", ErrNewCircuitBreaker)
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
			window: make([]Counts, 1, frames),
		},
		summary: &summary{
			counts: Counts{},
		},
	}

	cancelCh := make(chan struct{})
	cancel = cancelFunc(cancelCh)
	go cb.renewFrame(cancelCh)
	go cb.renewWindow(cancelCh)

	return cb, cancel, nil
}

func (c *CircuitBreaker) renewFrame(cancel <-chan struct{}) {
	for {
		select {
		case <-time.After(c.cfg.windowFrame):
			if c.state.s == HalfOpen {
				return
			}
			c.addFrame()
		case <-cancel:
			return
		}
	}
}

func (c *CircuitBreaker) renewWindow(cancel <-chan struct{}) {
	for {
		select {
		case <-time.After(c.cfg.windowRoll):
			if c.state.s == HalfOpen {
				return
			}
			c.unshiftWindow()
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
	switch c.state.s {
	case Closed:
		if c.canTrip(c.summary.counts) {
			c.state.s = Open
		}

	case Open:
		if c.onHalfOpenTimeout.Load() {
			return
		}
		go c.waitHalfOpen()

	case HalfOpen:
		lastFrame := c.rollingWindow.window[(len(c.rollingWindow.window) - 1)]

		switch c.fromHalfOpenToState(lastFrame) {
		case Open:
			if c.onHalfOpenTimeout.Load() {
				return
			}
			go c.waitHalfOpen()

			c.setState(Open)
			c.popWindow()

		case Closed:
			c.setState(Closed)
			c.popWindow()
		}
	}
}

func (c *CircuitBreaker) waitHalfOpen() {
	c.onHalfOpenTimeout.Store(true)
	<-time.After(c.cfg.halfOpenTimeout)
	c.setState(HalfOpen)
	c.onHalfOpenTimeout.Store(false)
}

func (c *CircuitBreaker) setState(s State) {
	c.state.mu.Lock()
	defer c.state.mu.Unlock()

	c.state.s = s
}

func (c *CircuitBreaker) canExecute() error {
	c.state.mu.Lock()
	defer c.state.mu.Unlock()

	if c.state.s == Open {
		return ErrOpenCircuit
	}

	if c.state.s == HalfOpen {
		return ErrHalfOpenCircuit
	}

	return nil
}

func (c *CircuitBreaker) addFrame() {
	c.rollingWindow.mu.Lock()
	defer c.rollingWindow.mu.Unlock()

	c.rollingWindow.window = append(c.rollingWindow.window, Counts{})
}

func (c *CircuitBreaker) unshiftWindow() {
	removedFrame := c.unshiftFrame()
	c.decrSummary(removedFrame.Fail, removedFrame.Success)
}

func (c *CircuitBreaker) popWindow() {
	removedFrame := c.popFrame()
	c.decrSummary(removedFrame.Fail, removedFrame.Success)
}

// unshiftFrame Removes the first frame from the rolling window.
func (c *CircuitBreaker) unshiftFrame() Counts {
	c.rollingWindow.mu.Lock()
	defer c.rollingWindow.mu.Unlock()

	removedFrame := c.rollingWindow.window[0]

	renewedWindow := make([]Counts, (len(c.rollingWindow.window) - 1))
	copy(renewedWindow, c.rollingWindow.window[1:])
	c.rollingWindow.window = renewedWindow

	return removedFrame
}

// popFrame Removes the last frame from the rolling window.
func (c *CircuitBreaker) popFrame() Counts {
	c.rollingWindow.mu.Lock()
	defer c.rollingWindow.mu.Unlock()

	i := (len(c.rollingWindow.window) - 1)

	removedFrame := c.rollingWindow.window[i]

	renewedWindow := make([]Counts, i)
	copy(renewedWindow, c.rollingWindow.window[:i])
	c.rollingWindow.window = renewedWindow

	return removedFrame
}

func (c *CircuitBreaker) incrSuccess() {
	c.rollingWindow.mu.Lock()
	defer c.rollingWindow.mu.Unlock()

	c.incrSummary(0, 1)

	i := (len(c.rollingWindow.window) - 1)
	c.rollingWindow.window[i].Total += 1
	c.rollingWindow.window[i].Success += 1
}

func (c *CircuitBreaker) incrFail() {
	c.rollingWindow.mu.Lock()
	defer c.rollingWindow.mu.Unlock()

	c.incrSummary(1, 0)

	i := (len(c.rollingWindow.window) - 1)
	c.rollingWindow.window[i].Total += 1
	c.rollingWindow.window[i].Fail += 1
}

func (c *CircuitBreaker) incrSummary(fail, success uint64) {
	c.summary.mu.Lock()
	defer c.summary.mu.Unlock()

	c.summary.counts.Fail += fail
	c.summary.counts.Success += success
	c.summary.counts.Total += (fail + success)
}

func (c *CircuitBreaker) decrSummary(fail, success uint64) {
	c.summary.mu.Lock()
	defer c.summary.mu.Unlock()

	c.summary.counts.Fail -= fail
	c.summary.counts.Success -= success
	c.summary.counts.Total -= (fail + success)
}
