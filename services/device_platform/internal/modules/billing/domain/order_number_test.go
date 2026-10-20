package domain

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNewOrderNumberFormat(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 20, 17, 45, 12, 0, time.UTC)
	pattern := regexp.MustCompile(`^SP20261020[A-Z2-7]{8}$`)
	number := NewOrderNumber(now)
	if !pattern.MatchString(number) {
		t.Fatalf("NewOrderNumber = %q, want SP20261020 + eight base32 characters", number)
	}
	if len(number) != 18 {
		t.Fatalf("order number length = %d, want 18", len(number))
	}
	if number[:2] != "SP" {
		t.Fatalf("order number prefix = %q, want SP", number[:2])
	}
	// RFC 4648 base32 has no 0, 1, 8, or 9, so a digit in the random segment is
	// always one of 2-7 and can never be mistaken for 0/1 when read aloud.
	if strings.ContainsAny(number[10:], "0189") {
		t.Fatalf("order number %q uses a character outside RFC 4648 base32", number)
	}
}

func TestNewOrderNumberUsesTheLocalDay(t *testing.T) {
	t.Parallel()

	// The date segment is the caller's clock day, so a caller deciding "today"
	// in another timezone gets that calendar day, not UTC's.
	location := time.FixedZone("UTC+8", 8*60*60)
	// 2026-10-19 23:30 UTC is 2026-10-20 07:30 in UTC+8.
	now := time.Date(2026, 10, 19, 23, 30, 0, 0, time.UTC).In(location)
	number := NewOrderNumber(now)
	if number[:10] != "SP20261020" {
		t.Fatalf("NewOrderNumber = %q, want date segment SP20261020", number)
	}
}

func TestNewRefundNumberFormat(t *testing.T) {
	t.Parallel()

	now := time.Date(2027, 1, 2, 3, 4, 5, 0, time.UTC)
	pattern := regexp.MustCompile(`^SR20270102[A-Z2-7]{8}$`)
	if number := NewRefundNumber(now); !pattern.MatchString(number) {
		t.Fatalf("NewRefundNumber = %q, want SR20270102 + eight base32 characters", number)
	}
}

func TestNewOrderNumberUniqueness(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	const iterations = 10000
	seen := make(map[string]struct{}, iterations)
	for index := 0; index < iterations; index++ {
		number := NewOrderNumber(now)
		if _, duplicate := seen[number]; duplicate {
			t.Fatalf("duplicate order number %q at iteration %d", number, index)
		}
		seen[number] = struct{}{}
	}
	if len(seen) != iterations {
		t.Fatalf("generated %d distinct numbers, want %d", len(seen), iterations)
	}
}

func TestNewOrderNumberIsUniqueUnderConcurrency(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	const (
		workers   = 8
		perWorker = 1250
	)
	results := make(chan string, workers*perWorker)
	var waitGroup sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			for index := 0; index < perWorker; index++ {
				results <- NewOrderNumber(now)
			}
		}()
	}
	waitGroup.Wait()
	close(results)

	// A shared or unsynchronised generator would repeat under load, so the
	// stress run asserts every concurrently minted number is distinct.
	seen := make(map[string]struct{}, workers*perWorker)
	for number := range results {
		if _, duplicate := seen[number]; duplicate {
			t.Fatalf("duplicate order number %q under concurrency", number)
		}
		seen[number] = struct{}{}
	}
	if len(seen) != workers*perWorker {
		t.Fatalf("generated %d distinct numbers, want %d", len(seen), workers*perWorker)
	}
}

func TestNewOrderNumberRandomSegmentIsNotSequential(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	// A counter or a seeded generator would repeat a suffix within a few draws;
	// the last two characters alone have 32*32 combinations, so among 100 draws
	// a well-mixed generator uses many of them.
	seen := make(map[string]struct{}, 100)
	for index := 0; index < 100; index++ {
		number := NewOrderNumber(now)
		seen[number[16:]] = struct{}{}
	}
	if len(seen) < 20 {
		t.Fatalf("random suffix shows only %d distinct values in 100 draws", len(seen))
	}
}

func ExampleNewOrderNumber() {
	number := NewOrderNumber(time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC))
	fmt.Println(len(number) == 18 && number[:10] == "SP20261020")
	// Output: true
}
