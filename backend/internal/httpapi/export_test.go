package httpapi

import "time"

// SetEventPoll makes the operation event stream poll faster in tests.
func SetEventPoll(d time.Duration) func() {
	old := eventPoll
	eventPoll = d
	return func() { eventPoll = old }
}
