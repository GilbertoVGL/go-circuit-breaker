package breaker

import (
	"errors"
	"log"
	"testing"
)

func fixtureCanTrip(c Counts) bool {
	log.Printf("summary:\n\tTotal:\t\t%v\n\tFail:\t\t%v\n\tSuccess:\t%v\n", c.Total, c.Fail, c.Success)
	f := float64(c.Fail)
	t := float64(c.Total)
	log.Printf("failed ratio: %+v\n", ((f / t) * 100))
	if ((f / t) * 100) >= 10 {
		log.Println("going to trip")
		return true
	}
	log.Println("no triping at all")

	return false
}

func fixtureCircuitCall() error {
	return errors.New("some error")
}

func Test_Breaker(t *testing.T) {
	br, cancel := New()
	defer cancel()

	br.summary.counts.Total = 99
	br.summary.counts.Fail = 9

	br.canTrip = fixtureCanTrip

	err := br.Execute(fixtureCircuitCall)
	t.Error(err)
}
