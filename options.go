package breaker

type options func(opt *optionsConfiguration)

type optionsConfiguration struct {
	windowFrame       int
	windowRoll        int
	halfOpenThreshold int

	fromHalfOpenToState fromHalfOpenToState
	canTrip             canTrip
}

func WithWindowFrameThreshold(frameSeconds int) options {
	return func(opt *optionsConfiguration) {
		opt.windowFrame = frameSeconds
	}
}

func WithWindowRollThreshold(rollSeconds int) options {
	return func(opt *optionsConfiguration) {
		opt.windowRoll = rollSeconds
	}
}

func WithHalOpenThreshold(halfOpenThreshold int) options {
	return func(opt *optionsConfiguration) {
		opt.halfOpenThreshold = halfOpenThreshold
	}
}

func WithCanTrip(canTrip canTrip) options {
	return func(opt *optionsConfiguration) {
		opt.canTrip = canTrip
	}
}

func WithFromHalfOpenToState(fromHalfOpenToState fromHalfOpenToState) options {
	return func(opt *optionsConfiguration) {
		opt.fromHalfOpenToState = fromHalfOpenToState
	}
}
