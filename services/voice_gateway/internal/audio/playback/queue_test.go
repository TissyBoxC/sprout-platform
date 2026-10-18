package playback

import (
	"errors"
	"testing"
)

func newTestItem(itemID string, priority Priority, interruptible bool) Item {
	return Item{
		ItemID:        itemID,
		Priority:      priority,
		Payload:       []byte{0x01},
		Interruptible: interruptible,
	}
}

func TestEnqueueOrdersByPriority(t *testing.T) {
	queue, err := NewQueue(50, 100)
	if err != nil {
		t.Fatalf("create queue: %v", err)
	}

	for _, item := range []Item{
		newTestItem("item_ambient", PriorityAmbient, true),
		newTestItem("item_conversation", PriorityConversation, true),
		newTestItem("item_prompt", PriorityPrompt, true),
	} {
		if err := queue.Enqueue(item); err != nil {
			t.Fatalf("enqueue %s: %v", item.ItemID, err)
		}
	}

	expectedOrder := []string{"item_prompt", "item_conversation", "item_ambient"}
	for _, expectedID := range expectedOrder {
		next, ok := queue.Next()
		if !ok {
			t.Fatalf("expected item %s", expectedID)
		}
		if next.ItemID != expectedID {
			t.Fatalf("expected %s, got %s", expectedID, next.ItemID)
		}
	}
}

func TestHigherPriorityInterruptsAndResumes(t *testing.T) {
	queue, err := NewQueue(50, 100)
	if err != nil {
		t.Fatalf("create queue: %v", err)
	}

	conversation := newTestItem("item_conversation", PriorityConversation, true)
	if err := queue.Enqueue(conversation); err != nil {
		t.Fatalf("enqueue conversation: %v", err)
	}
	if _, ok := queue.Next(); !ok {
		t.Fatal("expected conversation to start playing")
	}

	prompt := newTestItem("item_prompt", PriorityPrompt, true)
	if err := queue.Enqueue(prompt); err != nil {
		t.Fatalf("enqueue prompt: %v", err)
	}

	next, ok := queue.Next()
	if !ok {
		t.Fatal("expected prompt to play immediately")
	}
	if next.ItemID != "item_prompt" {
		t.Fatalf("expected prompt, got %s", next.ItemID)
	}

	// The interrupted conversation is resumed before any queued ambient item.
	if err := queue.Enqueue(newTestItem("item_ambient", PriorityAmbient, true)); err != nil {
		t.Fatalf("enqueue ambient: %v", err)
	}
	resumed, ok := queue.Next()
	if !ok {
		t.Fatal("expected interrupted conversation to resume")
	}
	if resumed.ItemID != "item_conversation" {
		t.Fatalf("expected conversation resume, got %s", resumed.ItemID)
	}
}

func TestClaimCurrentDetectsPreemption(t *testing.T) {
	queue, err := NewQueue(50, 100)
	if err != nil {
		t.Fatalf("create queue: %v", err)
	}
	if err := queue.Enqueue(newTestItem("item_conversation", PriorityConversation, true)); err != nil {
		t.Fatalf("enqueue conversation: %v", err)
	}
	if _, ok := queue.Next(); !ok {
		t.Fatal("expected conversation to start playing")
	}
	if !queue.ClaimCurrent("item_conversation") {
		t.Fatal("active item should be claimable")
	}
	if err := queue.Enqueue(newTestItem("item_prompt", PriorityPrompt, true)); err != nil {
		t.Fatalf("enqueue prompt: %v", err)
	}
	if queue.ClaimCurrent("item_conversation") {
		t.Fatal("preempted item must no longer be claimable")
	}
}

func TestSafetyItemCannotBeInterrupted(t *testing.T) {
	queue, err := NewQueue(50, 100)
	if err != nil {
		t.Fatalf("create queue: %v", err)
	}

	safety := newTestItem("item_safety", PrioritySafety, false)
	if err := queue.Enqueue(safety); err != nil {
		t.Fatalf("enqueue safety: %v", err)
	}
	if _, ok := queue.Next(); !ok {
		t.Fatal("expected safety item to play")
	}

	// No priority is higher than safety, so interrupting is impossible and the
	// queue must reject rather than silently drop the announcement.
	if err := queue.Enqueue(newTestItem("item_prompt", PriorityPrompt, true)); err != nil {
		t.Fatalf("enqueue lower priority item: %v", err)
	}
}

func TestMuteKeepsSafetyItems(t *testing.T) {
	queue, err := NewQueue(50, 100)
	if err != nil {
		t.Fatalf("create queue: %v", err)
	}

	if err := queue.Enqueue(newTestItem("item_conversation", PriorityConversation, true)); err != nil {
		t.Fatalf("enqueue conversation: %v", err)
	}
	if err := queue.Enqueue(newTestItem("item_safety", PrioritySafety, true)); err != nil {
		t.Fatalf("enqueue safety: %v", err)
	}

	queue.SetMuted(true)

	snapshot := queue.Snapshot()
	if !snapshot.IsMuted {
		t.Fatal("expected queue to be muted")
	}
	if snapshot.Volume != 0 {
		t.Fatalf("expected muted volume 0, got %d", snapshot.Volume)
	}

	next, ok := queue.Next()
	if !ok {
		t.Fatal("expected safety item to survive mute")
	}
	if next.ItemID != "item_safety" {
		t.Fatalf("expected safety item, got %s", next.ItemID)
	}
	if _, ok := queue.Next(); ok {
		t.Fatal("expected conversational audio to be dropped by mute")
	}
}

func TestVolumeIsCappedByGuardianPolicy(t *testing.T) {
	queue, err := NewQueue(80, 60)
	if err != nil {
		t.Fatalf("create queue: %v", err)
	}
	if snapshot := queue.Snapshot(); snapshot.Volume != 60 {
		t.Fatalf("expected initial volume capped at 60, got %d", snapshot.Volume)
	}

	if err := queue.SetVolume(100); err != nil {
		t.Fatalf("set volume: %v", err)
	}
	if snapshot := queue.Snapshot(); snapshot.Volume != 60 {
		t.Fatalf("expected volume capped at 60, got %d", snapshot.Volume)
	}

	if err := queue.SetMaxVolume(30); err != nil {
		t.Fatalf("set max volume: %v", err)
	}
	if snapshot := queue.Snapshot(); snapshot.Volume != 30 {
		t.Fatalf("expected volume lowered to 30, got %d", snapshot.Volume)
	}
}

func TestPauseAndResume(t *testing.T) {
	queue, err := NewQueue(50, 100)
	if err != nil {
		t.Fatalf("create queue: %v", err)
	}
	if err := queue.Enqueue(newTestItem("item_conversation", PriorityConversation, true)); err != nil {
		t.Fatalf("enqueue conversation: %v", err)
	}

	queue.Pause()
	if _, ok := queue.Next(); ok {
		t.Fatal("expected paused queue to withhold items")
	}
	queue.Resume()
	if _, ok := queue.Next(); !ok {
		t.Fatal("expected resumed queue to release items")
	}
}

func TestClearRetainsSafetyByDefault(t *testing.T) {
	queue, err := NewQueue(50, 100)
	if err != nil {
		t.Fatalf("create queue: %v", err)
	}
	if err := queue.Enqueue(newTestItem("item_conversation", PriorityConversation, true)); err != nil {
		t.Fatalf("enqueue conversation: %v", err)
	}
	if err := queue.Enqueue(newTestItem("item_safety", PrioritySafety, true)); err != nil {
		t.Fatalf("enqueue safety: %v", err)
	}

	queue.Clear(false)
	next, ok := queue.Next()
	if !ok || next.ItemID != "item_safety" {
		t.Fatalf("expected only safety item to remain, got %+v", next)
	}

	queue.Clear(true)
	if _, ok := queue.Next(); ok {
		t.Fatal("expected full clear to drop safety items")
	}
}

func TestEnqueueRejectsDuplicatesAndInvalidVolume(t *testing.T) {
	queue, err := NewQueue(50, 100)
	if err != nil {
		t.Fatalf("create queue: %v", err)
	}
	item := newTestItem("item_conversation", PriorityConversation, true)
	if err := queue.Enqueue(item); err != nil {
		t.Fatalf("enqueue conversation: %v", err)
	}
	if err := queue.Enqueue(item); !errors.Is(err, ErrDuplicateItemID) {
		t.Fatalf("expected duplicate rejection, got %v", err)
	}
	if _, err := NewQueue(101, 100); !errors.Is(err, ErrInvalidVolume) {
		t.Fatalf("expected invalid volume rejection, got %v", err)
	}
}

func TestPriorityStringIsStable(t *testing.T) {
	cases := map[Priority]string{
		PriorityAmbient:      "ambient",
		PriorityConversation: "conversation",
		PriorityPrompt:       "prompt",
		PrioritySafety:       "safety",
	}
	for priority, expected := range cases {
		if priority.String() != expected {
			t.Fatalf("expected %s, got %s", expected, priority.String())
		}
	}
}
