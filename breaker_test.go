package breaker

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errCall = errors.New("execute error")

func TestBreakerCreationSuccess(t *testing.T) {
	type expected struct {
		onHalfOpenTimeout bool
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
				summ:              Counts{},
				windowRoll:        time.Second * _windowRoll,
				windowFrame:       time.Second * _windowFrame,
				halfOpenTimeout:   time.Second * _halfOpenTimeout,
				window:            make([]Counts, 20, 22),
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
				summ:              Counts{},
				windowRoll:        time.Second * 100000,
				windowFrame:       time.Second * 1000,
				halfOpenTimeout:   time.Second * 10,
				window:            make([]Counts, 100, 102),
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
				summ:              Counts{},
				windowRoll:        time.Second * 4759,
				windowFrame:       time.Second * 222,
				halfOpenTimeout:   time.Second * 21,
				window:            make([]Counts, 21, 23),
			},
		},
		{
			name: "creates_with_can_trip",
			input: []option{
				WithCanTrip(func(summary Counts) bool { return true }),
			},
			expected: expected{
				onHalfOpenTimeout: false,
				summ:              Counts{},
				windowRoll:        time.Second * _windowRoll,
				windowFrame:       time.Second * _windowFrame,
				halfOpenTimeout:   time.Second * _halfOpenTimeout,
				window:            make([]Counts, 20, 22),
			},
		},
		{
			name: "creates_with_from_half_open_to_state",
			input: []option{
				WithFromHalfOpenToState(func(summary Counts) State { return Open }),
			},
			expected: expected{
				onHalfOpenTimeout: false,
				summ:              Counts{},
				windowRoll:        time.Second * _windowRoll,
				windowFrame:       time.Second * _windowFrame,
				halfOpenTimeout:   time.Second * _halfOpenTimeout,
				window:            make([]Counts, 20, 22),
			},
		},
	}
	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			cb, cancel, err := New(tc.input...)
			require.NoError(t, err)
			cancel()

			gotWindow := cb.windowCopy()
			assert.Equal(t, tc.expected.onHalfOpenTimeout, cb.onHalfOpenTimeout.Load())
			assert.Equal(t, tc.expected.summ, cb.summaryCopy())
			assert.Equal(t, tc.expected.windowRoll, cb.cfg.windowRoll)
			assert.Equal(t, tc.expected.windowFrame, cb.cfg.windowFrame)
			assert.Equal(t, tc.expected.halfOpenTimeout, cb.cfg.halfOpenTimeout)
			assert.ElementsMatch(t, tc.expected.window, gotWindow)
			assert.Equal(t, len(tc.expected.window), len(gotWindow))
			assert.Equal(t, cap(tc.expected.window), cap(gotWindow))
			assert.NotNil(t, cb.canTrip)
			assert.NotNil(t, cb.fromHalfOpenToState)
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
	expectedCounts := Counts{
		Total:   11,
		Fail:    7,
		Success: 4,
	}
	expectedWindow := make([]Counts, 100, 102)
	expectedWindow[99] = expectedCounts

	cb, cancel, err := New(
		WithWindowFrameThreshold(1000),
		WithWindowRollThreshold(100000),
		WithHalfOpenThreshold(10),
	)
	require.NoError(t, err)
	defer cancel()

	calls := []error{
		errCall, errCall, errCall, errCall, errCall, errCall, errCall,
		nil, nil, nil, nil, nil, nil,
	}

	syncFeedCircuitBreakerHelper(cb, calls, false)

	gotWindow := cb.windowCopy()
	assert.Equal(t, Open, cb.stateCopy())
	assert.Equal(t, expectedCounts, cb.summaryCopy())
	assert.Equal(t, expectedWindow[99], cb.currentFrameCopy())
	assert.ElementsMatch(t, expectedWindow, gotWindow)
	assert.Equal(t, len(expectedWindow), len(gotWindow))
	assert.Equal(t, cap(expectedWindow), cap(gotWindow))
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
	expectedWindow := make([]Counts, 4, 5)
	expectedWindow[3] = expectedCounts

	cb, cancel, err := New(
		WithWindowFrameThreshold(10),
		WithWindowRollThreshold(30),
		WithHalfOpenThreshold(2),
	)
	require.NoError(t, err)
	defer cancel()

	syncFeedCircuitBreakerHelper(cb, calls, false)

	time.Sleep(cb.cfg.halfOpenTimeout + (time.Millisecond * 500))

	gotWindow := cb.windowCopy()
	assert.Equal(t, HalfOpen, cb.stateCopy())
	assert.Equal(t, expectedCounts, cb.summaryCopy())
	assert.ElementsMatch(t, expectedWindow, gotWindow)
	assert.Equal(t, len(expectedWindow), len(gotWindow))
	assert.Equal(t, cap(expectedWindow), cap(gotWindow))

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
	expectedWindow := make([]Counts, 3, 5)
	expectedWindow[2] = expectedCounts

	cb, cancel, err := New(
		WithWindowFrameThreshold(10),
		WithWindowRollThreshold(30),
		WithHalfOpenThreshold(2),
	)
	require.NoError(t, err)
	defer cancel()

	syncFeedCircuitBreakerHelper(cb, calls, false)

	assert.Equal(t, Open, cb.stateCopy())

	time.Sleep(cb.cfg.halfOpenTimeout + (time.Millisecond * 500))

	assert.Equal(t, HalfOpen, cb.stateCopy())

	err = cb.Execute(fixtureCircuitCall(errCall))

	gotWindow := cb.windowCopy()
	assert.Equal(t, Open, cb.stateCopy())
	assert.Equal(t, expectedCounts, cb.summary.counts)
	assert.ElementsMatch(t, expectedWindow, gotWindow)
	assert.Equal(t, len(expectedWindow), len(gotWindow))
	assert.Equal(t, cap(expectedWindow), cap(gotWindow))
	assert.ErrorIs(t, err, errCall)
}

func TestBreakerHalfOpenToClosed(t *testing.T) {
	closedCalls := []error{
		errCall, errCall, errCall, errCall, errCall, errCall, errCall,
		nil, nil, nil, nil,
	}
	halfOpenCalls := make([]error, 52)

	expectedCounts := Counts{
		Total:   63,
		Fail:    7,
		Success: 56,
	}
	expectedWindow := make([]Counts, 3, 5)
	expectedWindow[2] = expectedCounts

	cb, cancel, err := New(
		WithWindowFrameThreshold(10),
		WithWindowRollThreshold(30),
		WithHalfOpenThreshold(2),
	)
	require.NoError(t, err)
	defer cancel()

	// call to open it
	syncFeedCircuitBreakerHelper(cb, closedCalls, false)

	assert.Equal(t, Open, cb.stateCopy())

	// wait for half open
	time.Sleep(cb.cfg.halfOpenTimeout + (time.Millisecond * 500))

	assert.Equal(t, HalfOpen, cb.stateCopy())

	// call to close it
	feedCircuitBreakerHelper(cb, halfOpenCalls, false)

	// wait for close
	time.Sleep(cb.cfg.halfOpenTimeout + (time.Millisecond * 500))

	t.Logf("totals:\t%+v\n", cb.summaryCopy())

	gotWindow := cb.windowCopy()
	assert.Equal(t, Closed, cb.stateCopy())
	assert.Equal(t, expectedCounts, cb.summaryCopy())
	assert.ElementsMatch(t, expectedWindow, gotWindow)
	assert.Equal(t, len(expectedWindow), len(gotWindow))
	assert.Equal(t, cap(expectedWindow), cap(gotWindow))
	assert.ErrorIs(t, err, nil)
}

func TestBreakerWindowRollSize(t *testing.T) {
	expectedWindow := make([]Counts, 10, 12)
	cb, cancel, err := New(
		WithWindowFrameThreshold(1),
		WithWindowRollThreshold(10),
		WithHalfOpenThreshold(2),
	)
	defer cancel()
	require.NoError(t, err)

	time.Sleep(time.Second * 20)

	gotWindow := cb.windowCopy()
	assert.Equal(t, len(expectedWindow), len(gotWindow))
	assert.Equal(t, cap(expectedWindow), cap(gotWindow))
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
		Total:   frame.Total,
		Fail:    frame.Fail,
		Success: frame.Success,
	}
	expectedWindow := make([]Counts, 3, 5)
	expectedWindow[0] = frame

	cb, cancel, err := New(
		WithWindowFrameThreshold(1),
		WithWindowRollThreshold(3),
		WithHalfOpenThreshold(2),
	)
	require.NoError(t, err)
	defer cancel()

	for i := 0; i < 3; i++ {
		feedCircuitBreakerHelper(cb, closedCalls, false)
		time.Sleep(cb.cfg.windowFrame + (time.Millisecond * 500))
	}

	gotWindow := cb.windowCopy()
	assert.Equal(t, Closed, cb.stateCopy())
	assert.Equal(t, expectedCounts, cb.summaryCopy())
	assert.ElementsMatch(t, expectedWindow, gotWindow)
	assert.Equal(t, len(expectedWindow), len(gotWindow))
	assert.Equal(t, cap(expectedWindow), cap(gotWindow))
}
