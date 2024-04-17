package breaker

func defaultCanTrip(summary Counts) bool {
	return summary.Total > 10 && ((float64(summary.Fail)/float64(summary.Total))*100) >= 60
}

func defaultFromHalfOpenToState(summary Counts) State {
	if summary.Fail > 0 {
		return Open
	}

	if summary.Total > 100 && (summary.Fail/summary.Total*100) < 99 {
		return Closed
	}

	return HalfOpen
}

func cancelFunc(cancelCh chan struct{}) func() {
	return func() {
		cancelCh <- struct{}{}
	}
}
