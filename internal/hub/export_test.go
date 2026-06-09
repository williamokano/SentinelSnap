package hub

import "time"

// SetHeartbeatInterval shortens the SSE heartbeat for tests and returns a
// function that restores the previous value.
func SetHeartbeatInterval(d time.Duration) (restore func()) {
	old := heartbeatInterval
	heartbeatInterval = d
	return func() { heartbeatInterval = old }
}
