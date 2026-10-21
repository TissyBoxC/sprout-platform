package service

import (
	"context"
	"errors"
	"testing"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/devicechild/domain"
)

type fakeStore struct {
	bindErr     error
	unbindErr   error
	binding     domain.Binding
	resolution  domain.Resolution
	resolveErr  error
	history     []domain.History
	boundWith   domain.Request
	unbindCalls int
}

func (store *fakeStore) Bind(
	_ context.Context,
	request domain.Request,
) (domain.Binding, error) {
	store.boundWith = request
	if store.bindErr != nil {
		return domain.Binding{}, store.bindErr
	}
	return domain.Binding{
		DeviceID: request.DeviceID,
		FamilyID: request.FamilyID,
		ChildID:  request.ChildID,
	}, nil
}

func (store *fakeStore) Resolve(
	context.Context,
	string,
) (domain.Resolution, error) {
	if store.resolveErr != nil {
		return domain.Resolution{}, store.resolveErr
	}
	return store.resolution, nil
}

func (store *fakeStore) Unbind(context.Context, string) error {
	store.unbindCalls++
	return store.unbindErr
}

func (store *fakeStore) History(
	context.Context,
	string,
) ([]domain.History, error) {
	return store.history, nil
}

func TestBindRejectsInvalidRequest(t *testing.T) {
	store := &fakeStore{}
	service := NewService(store, nil)
	if _, err := service.Bind(context.Background(), domain.Request{}); err == nil {
		t.Fatal("expected an error")
	}
	if store.boundWith.DeviceID != "" {
		t.Fatal("store must not be reached for an invalid request")
	}
}

func TestBindMapsForeignChildToUnknownChild(t *testing.T) {
	// The store refuses a child from another family; the service must expose it
	// as "unknown child" so the transport cannot distinguish a missing child
	// from one that belongs to someone else.
	store := &fakeStore{bindErr: domain.ErrChildNotInFamily}
	service := NewService(store, nil)
	_, err := service.Bind(context.Background(), domain.Request{
		FamilyID: "family-1",
		DeviceID: "device-1",
		ChildID:  "child-2",
	})
	if !errors.Is(err, ErrUnknownChild) {
		t.Fatalf("err = %v, want ErrUnknownChild", err)
	}
}

func TestBindSucceeds(t *testing.T) {
	store := &fakeStore{}
	service := NewService(store, nil)
	binding, err := service.Bind(context.Background(), domain.Request{
		FamilyID: "family-1",
		DeviceID: "device-1",
		ChildID:  "child-1",
	})
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	if binding.ChildID != "child-1" {
		t.Fatalf("child = %s", binding.ChildID)
	}
}

func TestUnbindMapsMissingActiveChild(t *testing.T) {
	store := &fakeStore{unbindErr: domain.ErrNoActiveChild}
	service := NewService(store, nil)
	err := service.Unbind(context.Background(), "device-1")
	if !errors.Is(err, ErrNothingToUnbind) {
		t.Fatalf("err = %v, want ErrNothingToUnbind", err)
	}
}

func TestResolvePassesThroughState(t *testing.T) {
	store := &fakeStore{
		resolution: domain.Resolution{
			State:      domain.ResolveUnassigned,
			ChildCount: 2,
		},
	}
	service := NewService(store, nil)
	resolution, err := service.Resolve(context.Background(), "device-1")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolution.State != domain.ResolveUnassigned {
		t.Fatalf("state = %q", resolution.State)
	}
	// The count is what lets the caller say "choose a child" instead of
	// "add a child".
	if resolution.ChildCount != 2 {
		t.Fatalf("child count = %d", resolution.ChildCount)
	}
}
