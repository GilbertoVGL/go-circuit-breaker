package breaker

import (
	"sync"
	"time"
)

type stateControler struct {
	halfOpenTimeout time.Duration
	active          bool
	threshold       int64

	mu sync.Mutex
}

func (sc *stateControler) defaultCanTrip(summary Counts) bool {
	return summary.Fail/summary.Total >= 60
}

func (sc *stateControler) defaultFromHalfOpenToState(summary Counts) State {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	now := time.Now().UTC().Unix()

	if !sc.active {
		sc.active = true
		sc.threshold = now + (int64(sc.halfOpenTimeout.Seconds()) / 2)

		return HalfOpen
	}

	if summary.Fail > 0 {
		sc.active = false
		return Open
	}

	if summary.Fail == 0 && now > sc.threshold {
		sc.active = false
		return Closed
	}

	return HalfOpen
}

func cancelFunc(cancelCh chan struct{}) func() {
	return func() {
		cancelCh <- struct{}{}
	}
}
