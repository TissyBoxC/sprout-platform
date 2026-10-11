package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ui_text/domain"
)

type fakeRepository struct {
	packages    map[string]*domain.PackageDetail
	revision    int64
	nextErr     error
	createErr   error
	created     int
	transitions []domain.TransitionInput
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{packages: map[string]*domain.PackageDetail{}}
}

func packageKey(id string, language string, platform domain.Platform, version int) string {
	return id + "|" + language + "|" + string(platform) + "|" + itoa(version)
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := make([]byte, 0, 8)
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}

func (r *fakeRepository) NextVersion(
	_ context.Context,
	packageID string,
	language string,
	platform domain.Platform,
) (int, error) {
	if r.nextErr != nil {
		return 0, r.nextErr
	}
	highest := 0
	for _, detail := range r.packages {
		if detail.Package.PackageID == packageID &&
			detail.Package.Language == language &&
			detail.Package.Platform == platform &&
			detail.Package.PackageVersion > highest {
			highest = detail.Package.PackageVersion
		}
	}
	return highest + 1, nil
}

func (r *fakeRepository) CreateVersion(
	_ context.Context,
	input domain.DraftInput,
	packageVersion int,
	actorID string,
	now time.Time,
) (*domain.PackageDetail, error) {
	if r.createErr != nil {
		return nil, r.createErr
	}
	r.created++
	detail := &domain.PackageDetail{
		Package: domain.Package{
			PackageID:        input.PackageID,
			PackageVersion:   packageVersion,
			Language:         input.Language,
			Platform:         input.Platform,
			MinClientVersion: input.MinClientVersion,
			Status:           domain.StatusDraft,
			CreatedBy:        actorID,
			CreatedAt:        now,
			UpdatedAt:        now,
		},
		Entries: append([]domain.Entry(nil), input.Entries...),
	}
	r.packages[packageKey(input.PackageID, input.Language, input.Platform, packageVersion)] = detail
	return detail, nil
}

func (r *fakeRepository) UpdateDraft(
	_ context.Context,
	input domain.DraftInput,
	packageVersion int,
	_ time.Time,
) (*domain.PackageDetail, error) {
	key := packageKey(input.PackageID, input.Language, input.Platform, packageVersion)
	detail, ok := r.packages[key]
	if !ok {
		return nil, domain.ErrPackageNotFound
	}
	if detail.Package.Status != domain.StatusDraft {
		return nil, domain.ErrInvalidTransition
	}
	detail.Entries = append([]domain.Entry(nil), input.Entries...)
	detail.Package.MinClientVersion = input.MinClientVersion
	return detail, nil
}

func (r *fakeRepository) GetVersion(
	_ context.Context,
	packageID string,
	language string,
	platform domain.Platform,
	packageVersion int,
) (*domain.PackageDetail, error) {
	detail, ok := r.packages[packageKey(packageID, language, platform, packageVersion)]
	if !ok {
		return nil, domain.ErrPackageNotFound
	}
	copied := *detail
	copied.Entries = append([]domain.Entry(nil), detail.Entries...)
	return &copied, nil
}

func (r *fakeRepository) ListVersions(
	_ context.Context,
	filter domain.VersionFilter,
) ([]domain.PackageVersion, int, error) {
	items := make([]domain.PackageVersion, 0, len(r.packages))
	for _, detail := range r.packages {
		if filter.PackageID != "" && detail.Package.PackageID != filter.PackageID {
			continue
		}
		if filter.Status != "" && detail.Package.Status != filter.Status {
			continue
		}
		items = append(items, domain.PackageVersion{
			Package:    detail.Package,
			EntryCount: len(detail.Entries),
		})
	}
	return items, len(items), nil
}

func (r *fakeRepository) ApplyTransition(
	_ context.Context,
	transition domain.TransitionInput,
	now time.Time,
) (*domain.Package, error) {
	key := packageKey(
		transition.PackageID,
		transition.Language,
		transition.Platform,
		transition.PackageVersion,
	)
	detail, ok := r.packages[key]
	if !ok {
		return nil, domain.ErrPackageNotFound
	}
	r.transitions = append(r.transitions, transition)
	switch transition.Action {
	case domain.ActionSubmit:
		detail.Package.Status = domain.StatusInReview
		detail.Package.SubmittedAt = &now
	case domain.ActionApprove:
		detail.Package.ApprovedAt = &now
		detail.Package.ApprovedBy = transition.ActorID
	case domain.ActionReject:
		detail.Package.Status = domain.StatusDraft
		detail.Package.SubmittedAt = nil
	case domain.ActionWithdraw:
		detail.Package.Status = domain.StatusWithdrawn
		detail.Package.WithdrawnAt = &now
	case domain.ActionPublish:
		r.revision++
		detail.Package.Status = domain.StatusPublished
		detail.Package.PublishedAt = &now
		detail.Package.SignatureKeyID = transition.SignatureKeyID
		detail.Package.Signature = transition.Signature
		detail.Package.Revision = r.revision
	default:
		return nil, domain.ErrInvalidTransition
	}
	detail.Package.UpdatedAt = now
	copied := detail.Package
	return &copied, nil
}

func (r *fakeRepository) CurrentRevision(context.Context) (int64, error) {
	return r.revision, nil
}

func (r *fakeRepository) ListPublished(
	_ context.Context,
	sinceRevision int64,
	_ int,
) (domain.RevisionPage, error) {
	page := domain.RevisionPage{Revision: r.revision}
	for _, detail := range r.packages {
		if detail.Package.Status == domain.StatusPublished &&
			detail.Package.Revision > sinceRevision {
			page.Packages = append(page.Packages, detail.Package)
		}
	}
	return page, nil
}

func (r *fakeRepository) PublishedPackage(
	_ context.Context,
	packageID string,
	language string,
	platform domain.Platform,
) (*domain.PublishedPackage, error) {
	for _, detail := range r.packages {
		if detail.Package.PackageID == packageID &&
			detail.Package.Language == language &&
			detail.Package.Platform == platform &&
			detail.Package.Status == domain.StatusPublished {
			return &domain.PublishedPackage{
				Package: detail.Package,
				Entries: append([]domain.Entry(nil), detail.Entries...),
			}, nil
		}
	}
	return nil, domain.ErrPackageNotFound
}

type recordingVerifier struct {
	err    error
	calls  int
	digest string
}

func (v *recordingVerifier) Verify(
	_ string,
	_ string,
	digest string,
	_ string,
) error {
	v.calls++
	v.digest = digest
	return v.err
}

type recordingAudit struct {
	actions []string
}

func (a *recordingAudit) RecordAudit(
	_ context.Context,
	_ string,
	_ string,
	action string,
	_ map[string]any,
) error {
	a.actions = append(a.actions, action)
	return nil
}

func validDraftInput() domain.DraftInput {
	return domain.DraftInput{
		PackageID:        "home_screen",
		Language:         "zh-CN",
		Platform:         domain.PlatformParentApp,
		MinClientVersion: "1.0.0",
		Entries: []domain.Entry{
			{UITextKey: "home.greeting", Value: "早上好", MaxLength: 32, Variables: []string{"child_name"}},
		},
	}
}

func newTestService(t *testing.T) (*Service, *fakeRepository, *recordingVerifier, *recordingAudit) {
	t.Helper()
	repository := newFakeRepository()
	verifier := &recordingVerifier{}
	audit := &recordingAudit{}
	service, err := New(Options{Repository: repository, Verifier: verifier, Audit: audit})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	return service, repository, verifier, audit
}

func transitionFor(version int, action domain.Action) domain.TransitionInput {
	return domain.TransitionInput{
		PackageID:      "home_screen",
		Language:       "zh-CN",
		Platform:       domain.PlatformParentApp,
		PackageVersion: version,
		Action:         action,
		ActorID:        "11111111-1111-1111-1111-111111111111",
	}
}

func TestCreateDraftRejectsInvalidPayload(t *testing.T) {
	service, repository, _, _ := newTestService(t)
	input := validDraftInput()
	input.Entries = nil
	if _, err := service.CreateDraft(context.Background(), input, "actor"); !errors.Is(err, domain.ErrInvalidPackage) {
		t.Fatalf("expected invalid package, got %v", err)
	}
	if repository.created != 0 {
		t.Fatal("invalid draft must not reach the repository")
	}
}

func TestPublishRequiresReviewabilityAndSignature(t *testing.T) {
	service, _, verifier, _ := newTestService(t)
	if _, err := service.CreateDraft(context.Background(), validDraftInput(), "actor"); err != nil {
		t.Fatalf("create draft: %v", err)
	}
	// Publishing straight from draft is not allowed.
	if _, err := service.Publish(
		context.Background(),
		transitionFor(1, domain.ActionPublish),
		domain.PublicationInput{SignatureKeyID: "ui-text-2026", Signature: "AAAA"},
	); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("expected invalid transition, got %v", err)
	}
	if _, err := service.Submit(context.Background(), transitionFor(1, domain.ActionSubmit)); err != nil {
		t.Fatalf("submit: %v", err)
	}
	// A malformed signature must be rejected before the verifier runs.
	if _, err := service.Publish(
		context.Background(),
		transitionFor(1, domain.ActionPublish),
		domain.PublicationInput{SignatureKeyID: "ui-text-2026", Signature: ""},
	); !errors.Is(err, domain.ErrSignatureRequired) {
		t.Fatalf("expected signature required, got %v", err)
	}
	if verifier.calls != 0 {
		t.Fatal("verifier must not run without a well-formed signature")
	}
	if _, err := service.Publish(
		context.Background(),
		transitionFor(1, domain.ActionPublish),
		domain.PublicationInput{SignatureKeyID: "ui-text-2026", Signature: "AAAA"},
	); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if verifier.calls != 1 || len(verifier.digest) != 64 {
		t.Fatalf("expected one verification with a sha256 digest, got %d %q", verifier.calls, verifier.digest)
	}
}

func TestPublishSurfacesVerifierFailure(t *testing.T) {
	service, _, verifier, _ := newTestService(t)
	verifier.err = domain.ErrSignatureInvalid
	if _, err := service.CreateDraft(context.Background(), validDraftInput(), "actor"); err != nil {
		t.Fatalf("create draft: %v", err)
	}
	if _, err := service.Submit(context.Background(), transitionFor(1, domain.ActionSubmit)); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if _, err := service.Publish(
		context.Background(),
		transitionFor(1, domain.ActionPublish),
		domain.PublicationInput{SignatureKeyID: "ui-text-2026", Signature: "AAAA"},
	); !errors.Is(err, domain.ErrSignatureInvalid) {
		t.Fatalf("expected signature invalid, got %v", err)
	}
}

func TestRejectRequiresReason(t *testing.T) {
	service, _, _, _ := newTestService(t)
	if _, err := service.CreateDraft(context.Background(), validDraftInput(), "actor"); err != nil {
		t.Fatalf("create draft: %v", err)
	}
	if _, err := service.Submit(context.Background(), transitionFor(1, domain.ActionSubmit)); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if _, err := service.Reject(context.Background(), transitionFor(1, domain.ActionReject), "   "); !errors.Is(err, domain.ErrReasonRequired) {
		t.Fatalf("expected reason required, got %v", err)
	}
	if _, err := service.Reject(context.Background(), transitionFor(1, domain.ActionReject), "标题过长"); err != nil {
		t.Fatalf("reject: %v", err)
	}
}

func TestPublishedRevisionAdvances(t *testing.T) {
	service, _, _, audit := newTestService(t)
	if _, err := service.CreateDraft(context.Background(), validDraftInput(), "actor"); err != nil {
		t.Fatalf("create draft: %v", err)
	}
	if revision, err := service.Revision(context.Background()); err != nil || revision != 0 {
		t.Fatalf("initial revision: %d, %v", revision, err)
	}
	if _, err := service.Submit(context.Background(), transitionFor(1, domain.ActionSubmit)); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if _, err := service.Publish(
		context.Background(),
		transitionFor(1, domain.ActionPublish),
		domain.PublicationInput{SignatureKeyID: "ui-text-2026", Signature: "AAAA"},
	); err != nil {
		t.Fatalf("publish: %v", err)
	}
	revision, err := service.Revision(context.Background())
	if err != nil || revision != 1 {
		t.Fatalf("revision after publish: %d, %v", revision, err)
	}
	page, err := service.Published(context.Background(), 0, 20)
	if err != nil {
		t.Fatalf("published: %v", err)
	}
	if len(page.Packages) != 1 || page.Packages[0].Status != domain.StatusPublished {
		t.Fatalf("unexpected published page: %+v", page.Packages)
	}
	published, err := service.PublishedPackage(
		context.Background(),
		"home_screen",
		"zh-CN",
		domain.PlatformParentApp,
	)
	if err != nil {
		t.Fatalf("published package: %v", err)
	}
	if published.Package.Signature != "AAAA" {
		t.Fatalf("published signature not persisted: %q", published.Package.Signature)
	}
	found := false
	for _, action := range audit.actions {
		if action == "admin.ui_text.published" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected publish audit, got %v", audit.actions)
	}
}

func TestPackageDigestIsOrderIndependent(t *testing.T) {
	left := &domain.PackageDetail{
		Package: domain.Package{
			PackageID:      "home_screen",
			PackageVersion: 3,
			Language:       "zh-CN",
			Platform:       domain.PlatformParentApp,
		},
		Entries: []domain.Entry{
			{UITextKey: "b.second", Value: "2", MaxLength: 8},
			{UITextKey: "a.first", Value: "1", MaxLength: 8},
		},
	}
	right := &domain.PackageDetail{
		Package: left.Package,
		Entries: []domain.Entry{
			{UITextKey: "a.first", Value: "1", MaxLength: 8},
			{UITextKey: "b.second", Value: "2", MaxLength: 8},
		},
	}
	if PackageDigest(left) != PackageDigest(right) {
		t.Fatal("digest must not depend on entry order")
	}
	right.Entries[0].Value = "changed"
	if PackageDigest(left) == PackageDigest(right) {
		t.Fatal("digest must change when an entry value changes")
	}
}
