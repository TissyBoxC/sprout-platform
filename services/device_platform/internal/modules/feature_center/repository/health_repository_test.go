package repository

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/feature_center/domain"
	"github.com/jackc/pgx/v5/pgconn"
)

type staticHealthExecer struct {
	tag pgconn.CommandTag
	err error
}

func (execer staticHealthExecer) Exec(
	context.Context,
	string,
	...any,
) (pgconn.CommandTag, error) {
	return execer.tag, execer.err
}

func TestSaveHealthRowsRejectsIgnoredStaleWrite(t *testing.T) {
	snapshot := &domain.HealthSnapshot{
		FeatureID: "voice_gateway",
		Status:    domain.HealthHealthy,
		CheckedAt: time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC),
		Message:   "功能运行正常",
		LatencyMS: 12,
		Trigger:   domain.HealthTriggerSchedule,
	}
	err := saveHealthRows(
		context.Background(),
		staticHealthExecer{tag: pgconn.NewCommandTag("INSERT 0 0")},
		snapshot,
	)
	if !errors.Is(err, domain.ErrHealthSnapshotStale) {
		t.Fatalf("ignored write = %v, want stale snapshot error", err)
	}
}

func TestSaveHealthRowsAcceptsPersistedWrite(t *testing.T) {
	snapshot := &domain.HealthSnapshot{
		FeatureID: "voice_gateway",
		Status:    domain.HealthHealthy,
		CheckedAt: time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC),
		Message:   "功能运行正常",
		LatencyMS: 12,
		Trigger:   domain.HealthTriggerManual,
	}
	err := saveHealthRows(
		context.Background(),
		staticHealthExecer{tag: pgconn.NewCommandTag("INSERT 0 1")},
		snapshot,
	)
	if err != nil {
		t.Fatalf("persisted write: %v", err)
	}
}

func TestSaveHealthRowsPropagatesDatabaseFailure(t *testing.T) {
	snapshot := &domain.HealthSnapshot{
		FeatureID: "voice_gateway",
		Status:    domain.HealthHealthy,
		CheckedAt: time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC),
		Message:   "功能运行正常",
		LatencyMS: 12,
		Trigger:   domain.HealthTriggerManual,
	}
	storageErr := errors.New("database unavailable")
	err := saveHealthRows(
		context.Background(),
		staticHealthExecer{err: storageErr},
		snapshot,
	)
	if !errors.Is(err, storageErr) {
		t.Fatalf("database failure = %v, want wrapped storage error", err)
	}
}

type leaseSessionStub struct {
	mu             sync.Mutex
	tryLockResults []leaseSessionResult
	unlockResults  []leaseSessionResult
	tryLockCalls   int
	unlockCalls    int
	releaseCalls   int
	destroyCalls   int
	discardCalls   int
	destroyErr     error
}

type leaseSessionResult struct {
	value bool
	err   error
}

func (session *leaseSessionStub) TryLock(
	context.Context,
	int64,
) (bool, error) {
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.tryLockCalls >= len(session.tryLockResults) {
		return false, errors.New("unexpected try-lock call")
	}
	result := session.tryLockResults[session.tryLockCalls]
	session.tryLockCalls++
	return result.value, result.err
}

func (session *leaseSessionStub) Unlock(
	context.Context,
	int64,
) (bool, error) {
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.unlockCalls >= len(session.unlockResults) {
		return false, errors.New("unexpected unlock call")
	}
	result := session.unlockResults[session.unlockCalls]
	session.unlockCalls++
	return result.value, result.err
}

func (session *leaseSessionStub) Release() {
	session.mu.Lock()
	session.releaseCalls++
	session.mu.Unlock()
}

func (session *leaseSessionStub) Discard(context.Context) error {
	session.mu.Lock()
	defer session.mu.Unlock()
	session.discardCalls++
	return nil
}

func (session *leaseSessionStub) Destroy(context.Context) error {
	session.mu.Lock()
	defer session.mu.Unlock()
	session.destroyCalls++
	return session.destroyErr
}

type leaseAcquirerStub struct {
	session leaderLeaseSession
	err     error
	calls   int
}

func (acquirer *leaseAcquirerStub) Acquire(
	context.Context,
) (leaderLeaseSession, error) {
	acquirer.calls++
	return acquirer.session, acquirer.err
}

func TestWithLeaderLeasePropagatesAcquireFailure(t *testing.T) {
	acquireErr := errors.New("pool closed")
	acquiredWork := false
	err := withLeaderLease(
		context.Background(),
		&leaseAcquirerStub{err: acquireErr},
		func(context.Context) error {
			acquiredWork = true
			return nil
		},
	)
	if !errors.Is(err, acquireErr) {
		t.Fatalf("acquire failure = %v, want wrapped pool error", err)
	}
	if acquiredWork {
		t.Fatal("work ran without a lease session")
	}
}

func TestWithLeaderLeaseDiscardsSessionWhenTryLockFails(t *testing.T) {
	tryLockErr := errors.New("advisory lock query failed")
	session := &leaseSessionStub{
		tryLockResults: []leaseSessionResult{{err: tryLockErr}},
	}
	workRan := false
	err := withLeaderLease(
		context.Background(),
		&leaseAcquirerStub{session: session},
		func(context.Context) error {
			workRan = true
			return nil
		},
	)
	if !errors.Is(err, tryLockErr) {
		t.Fatalf("try-lock failure = %v, want wrapped query error", err)
	}
	if workRan {
		t.Fatal("work ran after the lock query failed")
	}
	if session.discardCalls != 1 ||
		session.releaseCalls != 0 ||
		session.destroyCalls != 0 {
		t.Fatalf(
			"failed lock lifecycle discard=%d release=%d destroy=%d, want 1/0/0",
			session.discardCalls,
			session.releaseCalls,
			session.destroyCalls,
		)
	}
}

func TestWithLeaderLeaseSkipsWorkWhenLockIsHeldElsewhere(t *testing.T) {
	session := &leaseSessionStub{
		tryLockResults: []leaseSessionResult{{value: false}},
	}
	acquiredWork := false
	err := withLeaderLease(
		context.Background(),
		&leaseAcquirerStub{session: session},
		func(context.Context) error {
			acquiredWork = true
			return nil
		},
	)
	if !errors.Is(err, domain.ErrSchedulerLeaseUnavailable) {
		t.Fatalf("busy lease = %v, want unavailable", err)
	}
	if acquiredWork {
		t.Fatal("non-leader work ran")
	}
	if session.releaseCalls != 1 ||
		session.discardCalls != 0 ||
		session.destroyCalls != 0 {
		t.Fatalf(
			"busy lease lifecycle release=%d discard=%d destroy=%d, want 1/0/0",
			session.releaseCalls,
			session.discardCalls,
			session.destroyCalls,
		)
	}
}

func TestWithLeaderLeaseRetriesUnlockBeforeRelease(t *testing.T) {
	unlockErr := errors.New("unlock interrupted")
	session := &leaseSessionStub{
		tryLockResults: []leaseSessionResult{{value: true}},
		unlockResults: []leaseSessionResult{
			{err: unlockErr},
			{value: true},
		},
	}
	workErr := errors.New("probe failed")
	err := withLeaderLease(
		context.Background(),
		&leaseAcquirerStub{session: session},
		func(context.Context) error {
			return workErr
		},
	)
	if !errors.Is(err, workErr) {
		t.Fatalf("work failure = %v, want original work error", err)
	}
	if session.unlockCalls != 2 {
		t.Fatalf("unlock calls = %d, want retry", session.unlockCalls)
	}
	if session.releaseCalls != 1 ||
		session.discardCalls != 0 ||
		session.destroyCalls != 0 {
		t.Fatalf(
			"lease lifecycle release=%d discard=%d destroy=%d, want 1/0/0",
			session.releaseCalls,
			session.discardCalls,
			session.destroyCalls,
		)
	}
}

func TestWithLeaderLeaseDiscardsSessionWhenUnlockCannotBeConfirmed(t *testing.T) {
	unlockErr := errors.New("connection lost")
	session := &leaseSessionStub{
		tryLockResults: []leaseSessionResult{{value: true}},
		unlockResults: []leaseSessionResult{
			{err: unlockErr},
			{value: false},
		},
	}
	leasedWorkErr := errors.New("lease callback failed")
	err := withLeaderLease(
		context.Background(),
		&leaseAcquirerStub{session: session},
		func(context.Context) error {
			return leasedWorkErr
		},
	)
	if !errors.Is(err, domain.ErrSchedulerLeaseReleaseFailed) {
		t.Fatalf("release failure = %v, want stable release error", err)
	}
	if !errors.Is(err, unlockErr) {
		t.Fatalf("release failure = %v, want unlock cause", err)
	}
	if !errors.Is(err, leasedWorkErr) {
		t.Fatalf("release failure = %v, want callback cause", err)
	}
	if session.destroyCalls != 1 {
		t.Fatalf("destroy calls = %d, want exactly one", session.destroyCalls)
	}
	if session.releaseCalls != 0 || session.discardCalls != 0 {
		t.Fatalf(
			"release calls=%d discard calls=%d, want destroy-only",
			session.releaseCalls,
			session.discardCalls,
		)
	}
}

func TestWithLeaderLeaseReportsDestroyFailureAfterUnlockFailure(t *testing.T) {
	destroyErr := errors.New("socket close timeout")
	session := &leaseSessionStub{
		tryLockResults: []leaseSessionResult{{value: true}},
		unlockResults: []leaseSessionResult{
			{err: errors.New("unlock failed")},
			{value: false},
		},
		destroyErr: destroyErr,
	}
	err := withLeaderLease(
		context.Background(),
		&leaseAcquirerStub{session: session},
		func(context.Context) error { return nil },
	)
	if !errors.Is(err, domain.ErrSchedulerLeaseReleaseFailed) {
		t.Fatalf("release failure = %v, want stable release error", err)
	}
	if !errors.Is(err, destroyErr) {
		t.Fatalf("release failure = %v, want wrapped discard error", err)
	}
}

type concurrentLeaseSessionStub struct {
	mu          sync.Mutex
	acquired    bool
	unlockCalls int
}

func (session *concurrentLeaseSessionStub) TryLock(
	context.Context,
	int64,
) (bool, error) {
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.acquired {
		return false, nil
	}
	session.acquired = true
	return true, nil
}

func (session *concurrentLeaseSessionStub) Unlock(
	context.Context,
	int64,
) (bool, error) {
	session.mu.Lock()
	defer session.mu.Unlock()
	session.unlockCalls++
	if !session.acquired {
		return false, nil
	}
	session.acquired = false
	return true, nil
}

func (session *concurrentLeaseSessionStub) Release() {}

func (session *concurrentLeaseSessionStub) Destroy(context.Context) error {
	session.mu.Lock()
	session.acquired = false
	session.mu.Unlock()
	return nil
}

func (session *concurrentLeaseSessionStub) Discard(context.Context) error {
	session.Release()
	return nil
}

func TestWithLeaderLeaseAllowsExactlyOneConcurrentWorker(t *testing.T) {
	session := &concurrentLeaseSessionStub{}
	started := make(chan struct{})
	releaseCh := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		defer func() {
			select {
			case <-releaseCh:
			default:
				close(releaseCh)
			}
		}()
		firstDone <- withLeaderLease(
			context.Background(),
			&leaseAcquirerStub{session: session},
			func(context.Context) error {
				close(started)
				<-releaseCh
				return nil
			},
		)
	}()
	<-started

	secondRan := false
	err := withLeaderLease(
		context.Background(),
		&leaseAcquirerStub{session: session},
		func(context.Context) error {
			secondRan = true
			return nil
		},
	)
	if !errors.Is(err, domain.ErrSchedulerLeaseUnavailable) {
		t.Fatalf("concurrent lease = %v, want unavailable", err)
	}
	if secondRan {
		t.Fatal("second worker ran under the same lease")
	}
	close(releaseCh)
	if err := <-firstDone; err != nil {
		t.Fatalf("first lease worker: %v", err)
	}
}
