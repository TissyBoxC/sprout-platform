package session

import "fmt"

// State identifies the current voice session phase.
type State string

const (
	StateIdle      State = "idle"
	StateListening State = "listening"
	StateThinking  State = "thinking"
	StateSpeaking  State = "speaking"
)

// Event identifies a state transition requested by the conversation pipeline.
type Event string

const (
	EventStartListening Event = "start_listening"
	EventStartThinking  Event = "start_thinking"
	EventStartSpeaking  Event = "start_speaking"
	// EventFinishTurn returns a session to listening after a turn produced no
	// reply audio; a turn that did speak finishes via EventStartListening.
	EventFinishTurn Event = "finish_turn"
	EventReset      Event = "reset"
)

// Transition applies one event and returns the next session state.
//
// Keeping the matrix explicit prevents transport and adapter code from
// inventing incompatible lifecycle sequences.
func (s State) Transition(event Event) (State, error) {
	switch event {
	case EventStartListening:
		if s == StateIdle || s == StateSpeaking {
			return StateListening, nil
		}
	case EventStartThinking:
		if s == StateListening {
			return StateThinking, nil
		}
	case EventStartSpeaking:
		if s == StateThinking {
			return StateSpeaking, nil
		}
	case EventFinishTurn:
		if s == StateThinking {
			return StateListening, nil
		}
	case EventReset:
		return StateIdle, nil
	}

	return s, fmt.Errorf("invalid voice session transition: %s -> %s", s, event)
}
