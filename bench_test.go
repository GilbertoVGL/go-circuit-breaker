package breaker

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func BenchmarkExecute(b *testing.B) {
	calls := []error{
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		errCall, errCall, errCall, errCall, errCall, errCall, errCall, errCall, errCall, errCall,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		errCall, errCall, errCall, errCall, errCall, errCall, errCall, errCall, errCall, errCall,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		errCall, errCall, errCall, errCall, errCall, errCall, errCall, errCall, errCall, errCall,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		errCall, errCall, errCall, errCall, errCall, errCall, errCall, errCall, errCall, errCall,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		errCall, errCall, errCall, errCall, errCall, errCall, errCall, errCall, errCall, errCall,
	}
	for i := 0; i < b.N; i++ {
		cb, cancel, err := New(
			WithWindowFrameThreshold(1),
			WithWindowRollThreshold(300),
			WithHalfOpenThreshold(2),
		)
		require.NoError(b, err)
		for _, err := range calls {
			_ = cb.Execute(fixtureCircuitCall(err))
		}
		cancel()
	}
}

func BenchmarkMoveWindow(b *testing.B) {
	for i := 0; i < b.N; i++ {
		cb, cancel, err := New(
			WithWindowFrameThreshold(1),
			WithWindowRollThreshold(300),
			WithHalfOpenThreshold(2),
		)
		require.NoError(b, err)
		cancel()
		cb.moveWindow()
	}
}
