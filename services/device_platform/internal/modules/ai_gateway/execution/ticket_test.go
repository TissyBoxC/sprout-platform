package execution

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// memoryTicketStore models the Redis state transitions used by the production
// store. It is concurrency-safe so the one-time and lease rules can be checked
// under the race detector without hiding atomicity inside a permissive fake.
type memoryTicketStore struct {
	mutex   sync.Mutex
	tickets map[string]Ticket
	leases  map[string]map[string]time.Time
	now     time.Time
	nextID  uint64
}

func newMemoryTicketStore(now time.Time) *memoryTicketStore {
	return &memoryTicketStore{
		tickets: make(map[string]Ticket),
		leases:  make(map[string]map[string]time.Time),
		now:     now,
	}
}

func (store *memoryTicketStore) Issue(
	_ context.Context,
	ticket Ticket,
	ttl time.Duration,
) (string, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if strings.TrimSpace(ticket.DeviceID) == "" ||
		strings.TrimSpace(ticket.ParentAccountID) == "" ||
		!ticketProjectPattern.MatchString(ticket.Project) ||
		!ticketSessionPattern.MatchString(ticket.SessionID) {
		return "", errors.New("invalid ticket metadata")
	}
	token := "ticket-" + ticket.SessionID
	ticket.Token = token
	ticket.ExpiresAt = store.now.Add(ttl)
	store.tickets[token] = ticket
	return token, nil
}

func (store *memoryTicketStore) Consume(
	_ context.Context,
	token string,
) (Ticket, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	ticket, exists := store.tickets[token]
	delete(store.tickets, token)
	if !exists || !ticket.ExpiresAt.After(store.now) {
		return Ticket{}, ErrTicketInvalid
	}
	return ticket, nil
}

func (store *memoryTicketStore) AcquireDeviceLease(
	_ context.Context,
	deviceID string,
	limit int,
	ttl time.Duration,
) (string, func(), error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	active := store.leases[deviceID]
	if active == nil {
		active = make(map[string]time.Time)
		store.leases[deviceID] = active
	}
	for token, expiresAt := range active {
		if !expiresAt.After(store.now) {
			delete(active, token)
		}
	}
	if len(active) >= limit {
		return "", nil, ErrTicketConcurrencyExceeded
	}
	store.nextID++
	leaseToken := fmt.Sprintf("lease-%d-%d", store.now.UnixNano(), store.nextID)
	active[leaseToken] = store.now.Add(ttl)
	var released atomic.Bool
	return leaseToken, func() {
		if !released.CompareAndSwap(false, true) {
			return
		}
		store.mutex.Lock()
		defer store.mutex.Unlock()
		delete(store.leases[deviceID], leaseToken)
		if len(store.leases[deviceID]) == 0 {
			delete(store.leases, deviceID)
		}
	}, nil
}

func TestTicketIsSingleUseAndExpires(t *testing.T) {
	now := time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)
	store := newMemoryTicketStore(now)
	ticket := Ticket{
		DeviceID:        "device-0001",
		ParentAccountID: "parent-0001",
		SessionID:       "session-0001",
		Project:         "voice",
		Model:           "model-a",
		Concurrency:     1,
	}
	token, err := store.Issue(context.Background(), ticket, time.Minute)
	if err != nil {
		t.Fatalf("issue ticket: %v", err)
	}
	consumed, err := store.Consume(context.Background(), token)
	if err != nil {
		t.Fatalf("consume ticket: %v", err)
	}
	if consumed.DeviceID != ticket.DeviceID ||
		consumed.ParentAccountID != ticket.ParentAccountID ||
		consumed.Model != ticket.Model {
		t.Fatalf("consumed ticket = %+v", consumed)
	}
	if _, err := store.Consume(context.Background(), token); !errors.Is(err, ErrTicketInvalid) {
		t.Fatalf("second consume = %v, want ErrTicketInvalid", err)
	}

	expiredToken, err := store.Issue(context.Background(), ticket, time.Second)
	if err != nil {
		t.Fatalf("issue expiring ticket: %v", err)
	}
	store.now = store.now.Add(2 * time.Second)
	if _, err := store.Consume(context.Background(), expiredToken); !errors.Is(err, ErrTicketInvalid) {
		t.Fatalf("expired consume = %v, want ErrTicketInvalid", err)
	}
}

func TestTicketConcurrentConsumeAllowsExactlyOneWinner(t *testing.T) {
	store := newMemoryTicketStore(time.Now())
	token, err := store.Issue(context.Background(), Ticket{
		DeviceID:        "device-0001",
		ParentAccountID: "parent-0001",
		SessionID:       "session-0001",
		Project:         "voice",
		Model:           "model-a",
		Concurrency:     20,
	}, time.Minute)
	if err != nil {
		t.Fatalf("issue ticket: %v", err)
	}
	const callers = 24
	var winners atomic.Int32
	var wait sync.WaitGroup
	wait.Add(callers)
	for range callers {
		go func() {
			defer wait.Done()
			if _, err := store.Consume(context.Background(), token); err == nil {
				winners.Add(1)
			}
		}()
	}
	wait.Wait()
	if winners.Load() != 1 {
		t.Fatalf("ticket winners = %d, want 1", winners.Load())
	}
}

func TestTicketLeaseRejectsOverLimitAndReleasesIdempotently(t *testing.T) {
	store := newMemoryTicketStore(time.Now())
	firstToken, first, err := store.AcquireDeviceLease(
		context.Background(),
		"device-0001",
		1,
		time.Minute,
	)
	if err != nil {
		t.Fatalf("first lease: %v", err)
	}
	if firstToken == "" {
		t.Fatal("first lease token is empty")
	}
	if _, _, err := store.AcquireDeviceLease(
		context.Background(),
		"device-0001",
		1,
		time.Minute,
	); !errors.Is(err, ErrTicketConcurrencyExceeded) {
		t.Fatalf("second lease = %v, want ErrTicketConcurrencyExceeded", err)
	}
	first()
	first()
	secondToken, second, err := store.AcquireDeviceLease(
		context.Background(),
		"device-0001",
		1,
		time.Minute,
	)
	if err != nil {
		t.Fatalf("lease after release: %v", err)
	}
	if secondToken == firstToken {
		t.Fatal("lease tokens must be unique")
	}
	second()
}

func TestStaleLeaseReleaseCannotRemoveNewLease(t *testing.T) {
	store := newMemoryTicketStore(time.Now())
	_, staleRelease, err := store.AcquireDeviceLease(
		context.Background(),
		"device-0001",
		1,
		time.Second,
	)
	if err != nil {
		t.Fatalf("first lease: %v", err)
	}
	store.mutex.Lock()
	store.now = store.now.Add(2 * time.Second)
	store.mutex.Unlock()

	_, currentRelease, err := store.AcquireDeviceLease(
		context.Background(),
		"device-0001",
		1,
		time.Minute,
	)
	if err != nil {
		t.Fatalf("new lease: %v", err)
	}
	defer currentRelease()
	staleRelease()

	if _, _, err := store.AcquireDeviceLease(
		context.Background(),
		"device-0001",
		1,
		time.Minute,
	); !errors.Is(err, ErrTicketConcurrencyExceeded) {
		t.Fatalf("stale release removed the new lease: %v", err)
	}
}

func TestTicketIssueRejectsUnsafeMetadata(t *testing.T) {
	store := &RedisTicketStore{}
	for name, ticket := range map[string]Ticket{
		"empty device": {
			ParentAccountID: "parent-0001",
			Project:         "voice",
			SessionID:       "session-0001",
			Model:           "model-a",
			Concurrency:     1,
		},
		"uppercase project": {
			DeviceID:        "device-0001",
			ParentAccountID: "parent-0001",
			Project:         "VOICE",
			SessionID:       "session-0001",
			Model:           "model-a",
			Concurrency:     1,
		},
		"path-like session": {
			DeviceID:        "device-0001",
			ParentAccountID: "parent-0001",
			Project:         "voice",
			SessionID:       "../session",
			Model:           "model-a",
			Concurrency:     1,
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := store.Issue(context.Background(), ticket, time.Minute); err == nil {
				t.Fatal("unsafe ticket metadata was accepted")
			}
		})
	}
}
