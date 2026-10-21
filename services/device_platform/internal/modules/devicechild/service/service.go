// Package service exposes device-to-child binding operations.
//
// 绑定与切换都以家庭为边界：监护人只能操作自己家庭的设备与儿童。设备端读取
// 「当前儿童」时不需要监护人会话，但也不会看到其他设备的绑定历史。
package service

import (
	"context"
	"errors"
	"log/slog"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/devicechild/domain"
)

// ErrDeviceNotFound is returned when the device has no binding row.
var ErrDeviceNotFound = errors.New("device not found")

// ErrUnknownChild is returned when the child does not belong to the family.
var ErrUnknownChild = errors.New("child does not belong to the device family")

// ErrNothingToUnbind is returned when the device already has no active child.
var ErrNothingToUnbind = errors.New("device has no active child")

// Store is the persistence the service needs.
type Store interface {
	Bind(ctx context.Context, request domain.Request) (domain.Binding, error)
	Resolve(ctx context.Context, deviceID string) (domain.Resolution, error)
	Unbind(ctx context.Context, deviceID string) error
	History(ctx context.Context, deviceID string) ([]domain.History, error)
}

// Service implements the binding rules on top of a Store.
type Service struct {
	store  Store
	logger *slog.Logger
}

// NewService creates the service.
func NewService(store Store, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{store: store, logger: logger}
}

// Bind makes a child the device's active child and records the switch.
func (service *Service) Bind(
	ctx context.Context,
	request domain.Request,
) (domain.Binding, error) {
	if err := request.Validate(); err != nil {
		return domain.Binding{}, err
	}
	binding, err := service.store.Bind(ctx, request)
	if err != nil {
		// The store cannot tell an unknown device from an unknown child without
		// leaking which one exists, so both map to the same refusal here.
		if errors.Is(err, domain.ErrChildNotInFamily) {
			return domain.Binding{}, ErrUnknownChild
		}
		return domain.Binding{}, err
	}
	service.logger.Info(
		"device child binding activated",
		"device_id", binding.DeviceID,
		"family_id", binding.FamilyID,
	)
	return binding, nil
}

// Resolve returns the device's active child, or the reason it has none.
func (service *Service) Resolve(
	ctx context.Context,
	deviceID string,
) (domain.Resolution, error) {
	resolution, err := service.store.Resolve(ctx, deviceID)
	if err != nil {
		return domain.Resolution{}, err
	}
	return resolution, nil
}

// Unbind clears the active child but keeps the history.
func (service *Service) Unbind(ctx context.Context, deviceID string) error {
	if err := service.store.Unbind(ctx, deviceID); err != nil {
		if errors.Is(err, domain.ErrNoActiveChild) {
			return ErrNothingToUnbind
		}
		return err
	}
	service.logger.Info("device child binding cleared", "device_id", deviceID)
	return nil
}

// History lists the children a device has served.
func (service *Service) History(
	ctx context.Context,
	deviceID string,
) ([]domain.History, error) {
	return service.store.History(ctx, deviceID)
}
