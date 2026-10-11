package vision

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/platform/clock"
)

// Service runs one bounded vision task end to end: validate, screen, call the
// model, sanitize, and store the short-lived result.
type Service struct {
	client     ModelClient
	store      *Store
	gate       *SafetyGate
	clock      clock.Clock
	authorizer DeviceAuthorizer
}

// Options contains vision service dependencies.
type Options struct {
	Client ModelClient
	Store  *Store
	Gate   *SafetyGate
	Clock  clock.Clock
	// Authorizer verifies that the guardian still permits camera/vision
	// processing for the device before a model call and that the caller owns
	// the result device. It is required: without it the internal vision
	// endpoints would trust a caller-supplied device identifier.
	Authorizer DeviceAuthorizer
}

// New creates the vision service.
func New(options Options) (*Service, error) {
	if options.Client == nil {
		return nil, errors.New("vision model client is required")
	}
	if options.Authorizer == nil {
		return nil, errors.New("vision device authorizer is required")
	}
	store := options.Store
	if store == nil {
		store = NewStore(options.Clock)
	}
	gate := options.Gate
	if gate == nil {
		gate = NewSafetyGate()
	}
	timeSource := options.Clock
	if timeSource == nil {
		timeSource = clock.SystemClock{}
	}
	return &Service{
		client:     options.Client,
		store:      store,
		gate:       gate,
		clock:      timeSource,
		authorizer: options.Authorizer,
	}, nil
}

// Analyze validates one task, applies the safety gate, calls the model, and
// stores the bounded result. It returns ErrContentRejected when the prompt or
// model output violates policy and ErrInvalidRequest for malformed metadata.
func (s *Service) Analyze(ctx context.Context, request Request) (Result, error) {
	if err := validateRequest(request); err != nil {
		return Result{}, err
	}
	if err := s.authorizeDevice(ctx, request.DeviceID); err != nil {
		return Result{}, err
	}
	request.Prompt = strings.TrimSpace(request.Prompt)
	if s.gate.RejectPrompt(request.Prompt) {
		return Result{}, ErrContentRejected
	}

	modelResult, err := s.client.Analyze(ctx, ModelRequest{
		TaskID:      request.TaskID,
		DeviceID:    request.DeviceID,
		TaskType:    request.TaskType,
		Prompt:      request.Prompt,
		FrameWidth:  request.FrameWidth,
		FrameHeight: request.FrameHeight,
		FrameBytes:  request.FrameBytes,
	})
	if err != nil {
		return Result{}, errors.Join(ErrModelUnavailable, err)
	}
	modelResult, ok := s.gate.SanitizeResult(modelResult)
	if !ok {
		return Result{}, ErrContentRejected
	}

	now := s.clock.Now().UTC()
	result := Result{
		TaskID:    request.TaskID,
		DeviceID:  request.DeviceID,
		TaskType:  request.TaskType,
		Model:     modelResult.Model,
		Labels:    modelResult.Labels,
		Summary:   modelResult.Summary,
		CreatedAt: now,
		ExpiresAt: now.Add(ResultRetention),
	}
	s.store.Save(result)
	return result, nil
}

// GetResult returns one short-lived result by task id, scoped to the device
// that owns it. A different device receives ErrResultNotFound so a leaked task
// identifier cannot expose another child's result.
func (s *Service) GetResult(ctx context.Context, taskID string, deviceID string) (Result, error) {
	taskID = strings.TrimSpace(taskID)
	deviceID = strings.TrimSpace(deviceID)
	if !taskIDPattern.MatchString(taskID) || !devicePattern.MatchString(deviceID) {
		return Result{}, ErrInvalidRequest
	}
	if err := s.authorizeDevice(ctx, deviceID); err != nil {
		return Result{}, err
	}
	result, ok := s.store.Get(deviceID, taskID)
	if !ok {
		return Result{}, ErrResultNotFound
	}
	return result, nil
}

// PruneExpired removes expired results and returns how many were evicted.
func (s *Service) PruneExpired() int {
	return s.store.Prune()
}

// authorizeDevice fails closed when the guardian has withdrawn vision consent
// or the device is unknown. A missing authorizer is a programming error; the
// service refuses to trust the caller-supplied device identifier.
func (s *Service) authorizeDevice(ctx context.Context, deviceID string) error {
	if s == nil || s.authorizer == nil {
		return ErrDeviceNotAuthorized
	}
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" || !devicePattern.MatchString(deviceID) {
		return ErrDeviceNotAuthorized
	}
	if err := s.authorizer.AuthorizeDevice(ctx, deviceID); err != nil {
		return ErrDeviceNotAuthorized
	}
	return nil
}

func validateRequest(request Request) error {
	request.TaskID = strings.TrimSpace(request.TaskID)
	request.DeviceID = strings.TrimSpace(request.DeviceID)
	if !taskIDPattern.MatchString(request.TaskID) {
		return ErrInvalidRequest
	}
	if !devicePattern.MatchString(request.DeviceID) {
		return ErrInvalidRequest
	}
	if !TaskTypeIsValid(request.TaskType) {
		return ErrUnsupportedTask
	}
	if utf8.RuneCountInString(strings.TrimSpace(request.Prompt)) > MaxPromptRunes {
		return ErrInvalidRequest
	}
	if !frameIsValid(request.FrameWidth, request.FrameHeight, request.FrameBytes) {
		return ErrInvalidRequest
	}
	return nil
}

func frameIsValid(width, height, byteSize int) bool {
	if width < 0 || width > MaxFrameDimension {
		return false
	}
	if height < 0 || height > MaxFrameDimension {
		return false
	}
	return byteSize >= 0 && byteSize <= MaxFrameBytes
}
