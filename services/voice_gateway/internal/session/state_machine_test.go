package session

import "testing"

func TestConversationLifecycle(t *testing.T) {
	state := StateIdle

	steps := []struct {
		event Event
		want  State
	}{
		{event: EventStartListening, want: StateListening},
		{event: EventStartThinking, want: StateThinking},
		{event: EventStartSpeaking, want: StateSpeaking},
		{event: EventReset, want: StateIdle},
	}

	for _, step := range steps {
		next, err := state.Transition(step.event)
		if err != nil {
			t.Fatalf("transition %s from %s failed: %v", step.event, state, err)
		}
		if next != step.want {
			t.Fatalf("transition %s: expected %s, got %s", step.event, step.want, next)
		}
		state = next
	}
}

func TestInvalidTransitionIsRejected(t *testing.T) {
	if _, err := StateIdle.Transition(EventStartThinking); err == nil {
		t.Fatal("expected idle -> thinking to be rejected")
	}
}

func TestFinishTurnReturnsThinkingToListening(t *testing.T) {
	next, err := StateThinking.Transition(EventFinishTurn)
	if err != nil {
		t.Fatalf("thinking -> finish_turn failed: %v", err)
	}
	if next != StateListening {
		t.Fatalf("expected thinking -> listening, got %s", next)
	}
}

func TestFinishTurnIsRejectedWhileSpeaking(t *testing.T) {
	// A turn that produced audio returns to listening via the speaking edge, so
	// finish_turn must not silently skip it.
	if _, err := StateSpeaking.Transition(EventFinishTurn); err == nil {
		t.Fatal("expected speaking -> finish_turn to be rejected")
	}
}
