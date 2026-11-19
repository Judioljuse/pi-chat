package server

import (
	"testing"
	"time"
)

// closeAll must wake inbox long-polls and end connections that subscribe afterwards,
// so http.Server.Shutdown during a restart does not wait for them.
func TestHubCloseAllWakesWaiters(t *testing.T) {
	h := newHub()
	c := h.wait("p")
	h.closeAll()
	select {
	case <-c:
	case <-time.After(time.Second):
		t.Fatal("waiter not woken")
	}
	select {
	case <-h.wait("q"):
	case <-time.After(time.Second):
		t.Fatal("late waiter not woken")
	}
	select {
	case <-h.subscribe("r", false).done:
	case <-time.After(time.Second):
		t.Fatal("late subscriber not closed")
	}
}
