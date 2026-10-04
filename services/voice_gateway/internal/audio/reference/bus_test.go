package reference

import "testing"

func TestBusPublishAndLatestCopies(t *testing.T) {
	bus := NewBus()
	frame := []int16{100, -200, 300}
	bus.Publish("device_a", frame)

	// Mutating the source must not change the stored frame.
	frame[0] = 999
	got := bus.Latest("device_a")
	if len(got) != 3 || got[0] != 100 || got[1] != -200 || got[2] != 300 {
		t.Fatalf("Latest() = %v, want a stable copy of the published frame", got)
	}

	// Mutating the returned slice must not change the stored frame.
	got[0] = 555
	if second := bus.Latest("device_a"); second[0] != 100 {
		t.Fatalf("Latest() returned shared backing array: %v", second)
	}
}

func TestBusClearsOnEmptyPublishAndClear(t *testing.T) {
	bus := NewBus()
	bus.Publish("device_a", []int16{1, 2})
	bus.Publish("device_a", nil)
	if got := bus.Latest("device_a"); got != nil {
		t.Fatalf("Latest() after empty publish = %v, want nil", got)
	}

	bus.Publish("device_a", []int16{1, 2})
	bus.Clear("device_a")
	if got := bus.Latest("device_a"); got != nil {
		t.Fatalf("Latest() after Clear = %v, want nil", got)
	}
}

func TestNilBusIsSafe(t *testing.T) {
	var bus *Bus
	bus.Publish("device_a", []int16{1})
	if got := bus.Latest("device_a"); got != nil {
		t.Fatalf("nil Bus Latest() = %v, want nil", got)
	}
	bus.Clear("device_a")
}
