package breaker

import (
	"math/rand"
	"sync"
	"time"
)

func fixtureCircuitCall(err error) func() error {
	return func() error {
		return err
	}
}

func feedCircuitBreakerHelper(cb *CircuitBreaker, calls []error, withJitter bool) {
	var wg sync.WaitGroup
	wg.Add(len(calls))
	for _, err := range calls {
		if withJitter {
			time.Sleep(time.Duration(rand.Int63n(int64(2))))
		}
		go func(execErr error) {
			defer wg.Done()
			_ = cb.Execute(fixtureCircuitCall(execErr))
		}(err)
	}
	wg.Wait()
}

func syncFeedCircuitBreakerHelper(cb *CircuitBreaker, calls []error, withJitter bool) {
	for _, err := range calls {
		if withJitter {
			time.Sleep(time.Duration(rand.Int63n(int64(2))))
		}
		_ = cb.Execute(fixtureCircuitCall(err))
	}
}
