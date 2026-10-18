package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/family/domain"
)

func TestCreateInvitationAuditFailureDoesNotPersistInvitation(t *testing.T) {
	repository := newFakeRepository()
	repository.auditErr = errors.New("audit unavailable")
	service := newService(
		t,
		repository,
		time.Date(2026, time.October, 13, 9, 0, 0, 0, time.UTC),
	)
	if _, err := service.BootstrapOwner(context.Background(), "family-1", "owner-1"); err != nil {
		t.Fatalf("bootstrap owner: %v", err)
	}
	if _, err := service.CreateInvitation(
		context.Background(),
		"family-1",
		"owner-1",
		"guardian@example.com",
		domain.RoleGuardian,
	); err == nil {
		t.Fatal("expected invitation audit failure")
	}
	if len(repository.invitations) != 0 {
		t.Fatalf("invitation was persisted despite audit failure: %+v", repository.invitations)
	}
}

func TestMemberMutationsCarryAuditAndFailAtomically(t *testing.T) {
	repository := newFakeRepository()
	now := time.Date(2026, time.October, 13, 9, 0, 0, 0, time.UTC)
	service := newService(t, repository, now)
	if _, err := service.BootstrapOwner(context.Background(), "family-1", "owner-1"); err != nil {
		t.Fatalf("bootstrap owner: %v", err)
	}
	invitation, err := service.CreateInvitation(
		context.Background(),
		"family-1",
		"owner-1",
		"member@example.com",
		domain.RoleViewer,
	)
	if err != nil {
		t.Fatalf("create invitation: %v", err)
	}
	if _, err := service.AcceptInvitation(
		context.Background(),
		"family-1",
		invitation.ID,
		"member-1",
		"member@example.com",
	); err != nil {
		t.Fatalf("accept invitation: %v", err)
	}
	repository.mu.Lock()
	auditCount := len(repository.audits)
	repository.mu.Unlock()
	if auditCount != 2 {
		t.Fatalf("audits = %d, want invitation create and accept audits", auditCount)
	}
	repository.auditErr = errors.New("audit unavailable")
	if _, err := service.UpdateMemberRole(
		context.Background(),
		"family-1",
		"owner-1",
		"member-1",
		domain.RoleGuardian,
	); err == nil {
		t.Fatal("expected role audit failure")
	}
	member, ok, err := repository.Member(context.Background(), "family-1", "member-1")
	if err != nil || !ok {
		t.Fatalf("reload member: ok=%v err=%v", ok, err)
	}
	if member.Role != domain.RoleViewer {
		t.Fatalf("role changed despite audit failure: %+v", member)
	}
	repository.auditErr = nil
	if err := service.RemoveMember(
		context.Background(),
		"family-1",
		"owner-1",
		"member-1",
	); err != nil {
		t.Fatalf("remove member: %v", err)
	}
	repository.mu.Lock()
	auditCount = len(repository.audits)
	audit := repository.audits[len(repository.audits)-1]
	invitationAudit := repository.audits[0]
	repository.mu.Unlock()
	if auditCount != 3 {
		t.Fatalf("audits = %d, want create, accept and removal", auditCount)
	}
	if audit.Action != domain.AuditMemberRemoved ||
		audit.TargetAccountID != "member-1" {
		t.Fatalf("unexpected removal audit: %+v", audit)
	}
	if _, exists := invitationAudit.Detail["email"]; exists {
		t.Fatal("invitation audit must not contain invitee email")
	}
}

func TestAcceptInvitationConcurrentWinnerCommitsOnlyOneMember(t *testing.T) {
	repository := newFakeRepository()
	now := time.Date(2026, time.October, 13, 9, 0, 0, 0, time.UTC)
	ownerService := newService(t, repository, now)
	if _, err := ownerService.BootstrapOwner(
		context.Background(),
		"family-1",
		"owner-1",
	); err != nil {
		t.Fatalf("bootstrap owner: %v", err)
	}
	invitation, err := ownerService.CreateInvitation(
		context.Background(),
		"family-1",
		"owner-1",
		"member@example.com",
		domain.RoleGuardian,
	)
	if err != nil {
		t.Fatalf("create invitation: %v", err)
	}

	steppingClock := &stepClock{now: now}
	first, err := New(Options{
		Repository: repository,
		Clock:      steppingClock,
	})
	if err != nil {
		t.Fatalf("create first service: %v", err)
	}
	second, err := New(Options{
		Repository: repository,
		Clock:      steppingClock,
	})
	if err != nil {
		t.Fatalf("create second service: %v", err)
	}

	results := make(chan error, 2)
	accept := func(service *Service, userID string) {
		_, acceptErr := service.AcceptInvitation(
			context.Background(),
			"family-1",
			invitation.ID,
			userID,
			"member@example.com",
		)
		results <- acceptErr
	}
	go accept(first, "member-a")
	go accept(second, "member-b")
	firstErr := <-results
	secondErr := <-results

	winners := 0
	for _, result := range []error{firstErr, secondErr} {
		switch {
		case result == nil:
			winners++
		case !errors.Is(result, domain.ErrInvitationConcurrent) &&
			!errors.Is(result, domain.ErrInvitationState):
			t.Fatalf("unexpected concurrent accept result: %v", result)
		}
	}
	if winners != 1 {
		t.Fatalf("winners = %d, want exactly one", winners)
	}
	repository.mu.Lock()
	acceptCalls := repository.acceptCalls
	members := make([]domain.Member, 0, len(repository.members))
	for _, member := range repository.members {
		members = append(members, member)
	}
	repository.mu.Unlock()
	if acceptCalls < 1 || acceptCalls > 2 {
		t.Fatalf("accept calls = %d, want one or two attempts", acceptCalls)
	}
	memberCount := 0
	for _, member := range members {
		if member.FamilyID == "family-1" && member.Role != domain.RoleOwner {
			memberCount++
		}
	}
	if memberCount != 1 {
		t.Fatalf("accepted members = %d, want one", memberCount)
	}
}
