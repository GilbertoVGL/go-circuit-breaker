package breaker

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

type state string

const (
	Closed   state = "closed"
	HalfOpen state = "half-open"
	Open     state = "open"

	_windowRoll  = 15
	_windowFrame = 300
)

var (
	errOpenCircuit = errors.New("circuit open")
)

type (
	circuitCall func() error
	canTrip     func(c Counts) bool
	stateSwitch func(old, new state)
)

type circuitState struct {
	s  state
	mu sync.RWMutex
}

type deadline struct {
	t  time.Time
	mu sync.Mutex
}

type CircuitBreaker struct {
	canTrip canTrip
	open    atomic.Bool
	state   circuitState

	toHalfOpen         time.Duration
	toHalfOpenDeadline deadline
	halfOpenAllowed    uint64

	countsTTL      time.Duration
	countsDeadline deadline
	summary        Counts

	windowRoll    time.Duration
	windowFrame   time.Duration
	rollingWindow []Counts

	mu sync.Mutex
}

type Counts struct {
	Total   uint64
	Fail    uint64
	Success uint64
}

func New() CircuitBreaker {
	frames := _windowFrame / _windowRoll
	return CircuitBreaker{
		windowRoll:    (time.Second * _windowRoll),
		windowFrame:   (time.Second * _windowFrame),
		rollingWindow: make([]Counts, frames),
	}
}

func (c *CircuitBreaker) Execute(fn circuitCall) error {
	c.toNewGeneration()

	state := c.updateBasedOnState()
	if state == Open {
		return errOpenCircuit
	}

	defer func() {
		if r := recover(); r != nil {
			c.incrFail()
			panic(r)
		}
	}()

	err := fn()
	if err != nil {
		c.incrFail()
	} else {
		c.incrSuccess()
	}

	return err
}

func (c *CircuitBreaker) updateBasedOnState() state {
	c.state.mu.RLock()
	defer c.state.mu.RUnlock()

	switch c.state.s {
	case Closed:
		if c.canTrip(c.summary) {
			c.switchState(Open)
		}

	case HalfOpen:
		if c.halfOpenAllowed > c.summary.Success {

		}

	case Open:
		c.toHalfOpenDeadline.mu.Lock()
		defer c.toHalfOpenDeadline.mu.Unlock()

		now := time.Now().UTC()
		if now.After(c.toHalfOpenDeadline.t) {
			c.switchState(HalfOpen)
			c.resetCounts()
		}
	}

	return c.state.s
}

func (c *CircuitBreaker) toNewGeneration() {
	c.countsDeadline.mu.Lock()
	defer c.countsDeadline.mu.Unlock()

	now := time.Now().UTC()
	if now.After(c.countsDeadline.t) {
		c.countsDeadline.t = now.Add(c.countsTTL)
		c.resetCounts()
	}
}

func (c *CircuitBreaker) switchState(new state) {
	c.state.mu.Lock()
	defer c.state.mu.Unlock()

	c.state.s = new
}

func (c *CircuitBreaker) resetCounts() {
	atomic.StoreUint64(&c.summary.Total, 0)
	atomic.StoreUint64(&c.summary.Success, 0)
	atomic.StoreUint64(&c.summary.Fail, 0)
}

func (c *CircuitBreaker) incrSuccess() {
	atomic.AddUint64(&c.summary.Total, 1)
	atomic.AddUint64(&c.summary.Success, 1)
}

func (c *CircuitBreaker) incrFail() {
	atomic.AddUint64(&c.summary.Total, 1)
	atomic.AddUint64(&c.summary.Fail, 1)
}

func (c *CircuitBreaker) moveWindow() {
	c.mu.Lock()
	defer c.mu.Unlock()
	frame := c.rollingWindow[0]
	c.summary.Fail -= frame.Fail
	c.summary.Success -= frame.Success
	c.summary.Total -= frame.Total
	c.rollingWindow = c.rollingWindow[1:]
	c.rollingWindow = append(c.rollingWindow, Counts{})
}
