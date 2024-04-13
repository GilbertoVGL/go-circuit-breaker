package breaker

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fixtureCircuitCall(err error) func() error {
	return func() error {
		return err
	}
}

func TestCircuitBreakerCreationSuccess(t *testing.T) {
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

func TestCircuitBreakerCreationFails(t *testing.T) {
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

func Test_BreakerOpen(t *testing.T) {
	cb, cancel, err := New(
		WithWindowFrameThreshold(1000),
		WithWindowRollThreshold(100000),
		WithHalfOpenThreshold(10),
	)
	require.NoError(t, err)
	defer cancel()

	callErr := errors.New("execute error")
	calls := []error{
		callErr, callErr, callErr, callErr, callErr, callErr, callErr,
		nil, nil, nil, nil,
	}

	for _, err := range calls {
		_ = cb.Execute(fixtureCircuitCall(err))
	}

	frame := cb.rollingWindow.window[0]

	err = cb.Execute(fixtureCircuitCall(nil))
	assert.ErrorIs(t, err, ErrOpenCircuit)
	assert.Equal(t, uint64(7), frame.Fail)
	assert.Equal(t, uint64(4), frame.Success)
	assert.Equal(t, uint64(11), frame.Total)
}
