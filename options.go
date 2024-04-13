package breaker

import "errors"

type option func(opt *optionsConfiguration) error

type optionsConfiguration struct {
	windowFrame       int
	windowRoll        int
	halfOpenThreshold int

	fromHalfOpenToState fromHalfOpenToState
	canTrip             canTrip
}

func WithWindowFrameThreshold(seconds int) option {
	return func(opt *optionsConfiguration) error {
		if seconds <= 0 {
			return errors.New("frame can't be less than equal zero")
		}
		opt.windowFrame = seconds
		return nil
	}
}

func WithWindowRollThreshold(seconds int) option {
	return func(opt *optionsConfiguration) error {
		if seconds <= 0 {
			return errors.New("window roll can't be less than equal zero")
		}
		opt.windowRoll = seconds
		return nil
	}
}

func WithHalfOpenThreshold(seconds int) option {
	return func(opt *optionsConfiguration) error {
		if seconds <= 0 {
			return errors.New("half open threshold can't be less than equal zero")
		}
		opt.halfOpenThreshold = seconds
		return nil
	}
}

func WithCanTrip(canTrip canTrip) option {
	return func(opt *optionsConfiguration) error {
		if canTrip == nil {
			return errors.New("can trip callback can't be <nil>")
		}
		opt.canTrip = canTrip
		return nil
	}
}

func WithFromHalfOpenToState(fromHalfOpenToState fromHalfOpenToState) option {
	return func(opt *optionsConfiguration) error {
		if fromHalfOpenToState == nil {
			return errors.New("half open state change callback can't be <nil>")
		}
		opt.fromHalfOpenToState = fromHalfOpenToState
		return nil
	}
}
