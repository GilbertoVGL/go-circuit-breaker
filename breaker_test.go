package breaker

import (
	"errors"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errCall = errors.New("execute error")

func fixtureCircuitCall(err error) func() error {
	return func() error {
		return err
	}
}

func TestBreakerCreationSuccess(t *testing.T) {
	type expected struct {
		onHalfOpenTimeout bool
		windowLen         int
		windowCap         int
		summ              Counts
		windowRoll        time.Duration
		windowFrame       time.Duration
		halfOpenTimeout   time.Duration
		window            []Counts
	}

	tt := []struct {
		name     string
		input    []option
		expected expected
	}{
		{
			name:  "creates_default",
			input: nil,
			expected: expected{
				onHalfOpenTimeout: false,
				windowLen:         1,
				windowCap:         20,
				summ:              Counts{},
				windowRoll:        time.Second * _windowRoll,
				windowFrame:       time.Second * _windowFrame,
				halfOpenTimeout:   time.Second * _halfOpenTimeout,
				window:            []Counts{{}},
			},
		},
		{
			name: "creates_with_thresholds",
			input: []option{
				WithWindowFrameThreshold(1000),
				WithWindowRollThreshold(100000),
				WithHalfOpenThreshold(10),
			},
			expected: expected{
				onHalfOpenTimeout: false,
				windowLen:         1,
				windowCap:         100,
				summ:              Counts{},
				windowRoll:        time.Second * 100000,
				windowFrame:       time.Second * 1000,
				halfOpenTimeout:   time.Second * 10,
				window:            []Counts{{}},
			},
		},
		{
			name: "creates_with_odd_thresholds_round_down_capacity",
			input: []option{
				WithWindowFrameThreshold(222),
				WithWindowRollThreshold(4759),
				WithHalfOpenThreshold(21),
			},
			expected: expected{
				onHalfOpenTimeout: false,
				windowLen:         1,
				windowCap:         21,
				summ:              Counts{},
				windowRoll:        time.Second * 4759,
				windowFrame:       time.Second * 222,
				halfOpenTimeout:   time.Second * 21,
				window:            []Counts{{}},
			},
		},
		{
			name: "creates_with_can_trip",
			input: []option{
				WithCanTrip(func(summary Counts) bool { return true }),
			},
			expected: expected{
				onHalfOpenTimeout: false,
				windowLen:         1,
				windowCap:         20,
				summ:              Counts{},
				windowRoll:        time.Second * _windowRoll,
				windowFrame:       time.Second * _windowFrame,
				halfOpenTimeout:   time.Second * _halfOpenTimeout,
				window:            []Counts{{}},
			},
		},
		{
			name: "creates_with_from_half_open_to_state",
			input: []option{
				WithFromHalfOpenToState(func(summary Counts) State { return Open }),
			},
			expected: expected{
				onHalfOpenTimeout: false,
				windowLen:         1,
				windowCap:         20,
				summ:              Counts{},
				windowRoll:        time.Second * _windowRoll,
				windowFrame:       time.Second * _windowFrame,
				halfOpenTimeout:   time.Second * _halfOpenTimeout,
				window:            []Counts{{}},
			},
		},
	}
	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			cb, cancel, err := New(tc.input...)
			require.NoError(t, err)

			cancel()

			assert.Equal(t, tc.expected.onHalfOpenTimeout, cb.onHalfOpenTimeout.Load())
			assert.Equal(t, tc.expected.windowLen, len(cb.rollingWindow.window))
			assert.Equal(t, tc.expected.windowCap, cap(cb.rollingWindow.window))
			assert.Equal(t, tc.expected.summ, cb.summary.counts)
			assert.Equal(t, tc.expected.windowRoll, cb.cfg.windowRoll)
			assert.Equal(t, tc.expected.windowFrame, cb.cfg.windowFrame)
			assert.Equal(t, tc.expected.halfOpenTimeout, cb.cfg.halfOpenTimeout)
			assert.NotNil(t, cb.canTrip)
			assert.NotNil(t, cb.fromHalfOpenToState)
			assert.ElementsMatch(t, tc.expected.window, cb.rollingWindow.window)
		})
	}
}

func TestBreakerCreationFails(t *testing.T) {
	tt := []struct {
		name     string
		input    []option
		expected error
	}{
		{
			name: "fail_when_frame_threshold_is_greater_than_roll",
			input: []option{
				WithWindowFrameThreshold(100000),
				WithWindowRollThreshold(1000),
				WithHalfOpenThreshold(10),
			},
			expected: ErrNewCircuitBreaker,
		},
		{
			name: "fail_when_frame_threshold_is_zero",
			input: []option{
				WithWindowFrameThreshold(0),
			},
			expected: ErrNewCircuitBreaker,
		},
		{
			name: "fail_when_frame_threshold_is_less_than_zero",
			input: []option{
				WithWindowFrameThreshold(-1000),
			},
			expected: ErrNewCircuitBreaker,
		},
		{
			name: "fail_when_window_roll_threshold_is_zero",
			input: []option{
				WithWindowRollThreshold(0),
			},
			expected: ErrNewCircuitBreaker,
		},
		{
			name: "fail_when_window_roll_threshold_is_less_than_zero",
			input: []option{
				WithWindowRollThreshold(-1000),
			},
			expected: ErrNewCircuitBreaker,
		},
		{
			name: "fail_when_half_open_threshold_is_zero",
			input: []option{
				WithHalfOpenThreshold(0),
			},
			expected: ErrNewCircuitBreaker,
		},
		{
			name: "fail_when_half_open_threshold_is_less_than_zero",
			input: []option{
				WithHalfOpenThreshold(-1000),
			},
			expected: ErrNewCircuitBreaker,
		},
		{
			name: "fail_when_can_trip_callback_is_nil",
			input: []option{
				WithCanTrip(nil),
			},
			expected: ErrNewCircuitBreaker,
		},
		{
			name: "fail_when_from_half_open_to_state_callback_is_nil",
			input: []option{
				WithFromHalfOpenToState(nil),
			},
			expected: ErrNewCircuitBreaker,
		},
	}
	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			cb, cancel, err := New(tc.input...)

			assert.Nil(t, cb)
			assert.Nil(t, cancel)
			assert.ErrorIs(t, err, tc.expected)
		})
	}
}

func TestBreakerOpen(t *testing.T) {
	cb, cancel, err := New(
		WithWindowFrameThreshold(1000),
		WithWindowRollThreshold(100000),
		WithHalfOpenThreshold(10),
	)
	require.NoError(t, err)
	defer cancel()

	calls := []error{
		errCall, errCall, errCall, errCall, errCall, errCall, errCall,
		nil, nil, nil, nil, nil,
	}

	expectedCounts := Counts{
		Total:   11,
		Fail:    7,
		Success: 4,
	}
	expectedWindow := make([]Counts, 1, (100000 / 1000))
	expectedWindow[0] = expectedCounts

	feedCircuitBreakerHelper(cb, calls, false)

	assert.Equal(t, cb.state.s, Open)
	assert.Equal(t, expectedCounts, cb.summary.counts)
	assert.ElementsMatch(t, expectedWindow, cb.rollingWindow.window)
	assert.Equal(t, len(expectedWindow), len(cb.rollingWindow.window))
	assert.Equal(t, cap(expectedWindow), cap(cb.rollingWindow.window))
}

func TestBreakerClosedToHalfOpen(t *testing.T) {
	calls := []error{
		errCall, errCall, errCall, errCall, errCall, errCall, errCall,
		nil, nil, nil, nil,
	}

	expectedCounts := Counts{
		Total:   11,
		Fail:    7,
		Success: 4,
	}
	expectedWindow := make([]Counts, 2, 3)
	expectedWindow[0] = expectedCounts

	cb, cancel, err := New(
		WithWindowFrameThreshold(10),
		WithWindowRollThreshold(30),
		WithHalfOpenThreshold(2),
	)
	require.NoError(t, err)
	defer cancel()

	feedCircuitBreakerHelper(cb, calls, false)

	time.Sleep(cb.cfg.halfOpenTimeout)

	assert.Equal(t, HalfOpen, cb.state.s)
	assert.Equal(t, expectedCounts, cb.summary.counts)
	assert.ElementsMatch(t, expectedWindow, cb.rollingWindow.window)
	assert.Equal(t, len(expectedWindow), len(cb.rollingWindow.window))
	assert.Equal(t, cap(expectedWindow), cap(cb.rollingWindow.window))

	err = cb.Execute(fixtureCircuitCall(nil))
	assert.NoError(t, err)
}

func TestBreakerHalfOpenToOpen(t *testing.T) {
	calls := []error{
		errCall, errCall, errCall, errCall, errCall, errCall, errCall,
		nil, nil, nil, nil,
	}

	expectedCounts := Counts{
		Total:   11,
		Fail:    7,
		Success: 4,
	}
	expectedWindow := make([]Counts, 1, 3)
	expectedWindow[0] = expectedCounts

	cb, cancel, err := New(
		WithWindowFrameThreshold(10),
		WithWindowRollThreshold(30),
		WithHalfOpenThreshold(2),
	)
	require.NoError(t, err)
	defer cancel()

	feedCircuitBreakerHelper(cb, calls, false)

	time.Sleep(cb.cfg.halfOpenTimeout)

	err = cb.Execute(fixtureCircuitCall(errCall))

	assert.Equal(t, Open, cb.state.s)
	assert.Equal(t, expectedCounts, cb.summary.counts)
	assert.ElementsMatch(t, expectedWindow, cb.rollingWindow.window)
	assert.Equal(t, len(expectedWindow), len(cb.rollingWindow.window))
	assert.Equal(t, cap(expectedWindow), cap(cb.rollingWindow.window))
	assert.ErrorIs(t, err, errCall)
}

func TestBreakerHalfOpenToClosed(t *testing.T) {
	closedCalls := []error{
		errCall, errCall, errCall, errCall, errCall, errCall, errCall,
		nil, nil, nil, nil,
	}
	halfOpenCalls := make([]error, 101)

	expectedCounts := Counts{
		Total:   11,
		Fail:    7,
		Success: 4,
	}
	expectedWindow := make([]Counts, 1, 3)
	expectedWindow[0] = expectedCounts

	cb, cancel, err := New(
		WithWindowFrameThreshold(10),
		WithWindowRollThreshold(30),
		WithHalfOpenThreshold(2),
	)
	require.NoError(t, err)
	defer cancel()

	feedCircuitBreakerHelper(cb, closedCalls, false)

	time.Sleep(cb.cfg.halfOpenTimeout)

	feedCircuitBreakerHelper(cb, halfOpenCalls, false)

	assert.Equal(t, Closed, cb.state.s)
	assert.Equal(t, expectedCounts, cb.summary.counts)
	assert.ElementsMatch(t, expectedWindow, cb.rollingWindow.window)
	assert.Equal(t, len(expectedWindow), len(cb.rollingWindow.window))
	assert.Equal(t, cap(expectedWindow), cap(cb.rollingWindow.window))
	assert.ErrorIs(t, err, nil)
}

func TestBreakerWindowRoll(t *testing.T) {
	closedCalls := []error{
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		errCall, errCall,
	}
	frame := Counts{
		Total:   15,
		Fail:    2,
		Success: 13,
	}
	expectedCounts := Counts{
		Total:   (frame.Total * 2),
		Fail:    (frame.Fail * 2),
		Success: (frame.Success * 2),
	}
	expectedWindow := make([]Counts, 3)
	expectedWindow[0] = frame
	expectedWindow[1] = frame
	expectedWindow[2] = Counts{}

	cb, cancel, err := New(
		WithWindowFrameThreshold(10),
		WithWindowRollThreshold(30),
		WithHalfOpenThreshold(2),
	)
	require.NoError(t, err)
	defer cancel()

	for i := 0; i < 3; i++ {
		feedCircuitBreakerHelper(cb, closedCalls, false)
		time.Sleep(cb.cfg.windowFrame)
	}

	assert.Equal(t, Closed, cb.state.s)
	assert.Equal(t, expectedCounts, cb.summary.counts)
	assert.ElementsMatch(t, expectedWindow, cb.rollingWindow.window)
	assert.Equal(t, len(expectedWindow), len(cb.rollingWindow.window))
	assert.Equal(t, cap(expectedWindow), cap(cb.rollingWindow.window))
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
