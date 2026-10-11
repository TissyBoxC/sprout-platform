package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	formfactordomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/form_factor/domain"
)

// formFactorTestService is an in-memory double so the handler tests exercise the
// real composition without a database.
type formFactorTestService struct {
	profiles map[string]formfactordomain.Profile
}

func (s *formFactorTestService) Report(
	_ context.Context,
	familyID string,
	deviceID string,
	input formfactordomain.Input,
) (formfactordomain.Profile, error) {
	normalized, err := formfactordomain.ValidateInput(input)
	if err != nil {
		return formfactordomain.Profile{}, err
	}
	if s.profiles == nil {
		s.profiles = map[string]formfactordomain.Profile{}
	}
	profile := formfactordomain.Profile{
		DeviceID:       deviceID,
		FamilyID:       familyID,
		TechnicalName:  normalized.TechnicalName,
		CapabilityMask: normalized.CapabilityMask,
		ActionMask:     formfactordomain.ComposeActions(normalized.CapabilityMask),
		SchemaVersion:  normalized.SchemaVersion,
	}
	s.profiles[deviceID] = profile
	return profile, nil
}

func (s *formFactorTestService) Capability(
	_ context.Context,
	_ string,
	deviceID string,
) (formfactordomain.CapabilitySummary, error) {
	profile, ok := s.profiles[deviceID]
	if !ok {
		return formfactordomain.CapabilitySummary{
			SchemaVersion: formfactordomain.SchemaVersion,
		}, nil
	}
	return formfactordomain.CapabilitySummary{
		Supported:      true,
		TechnicalName:  profile.TechnicalName,
		CapabilityMask: profile.CapabilityMask,
		ActionMask:     profile.ActionMask,
		Capabilities:   formfactordomain.Capabilities(profile.CapabilityMask),
		Actions:        formfactordomain.Actions(profile.ActionMask),
		SchemaVersion:  profile.SchemaVersion,
	}, nil
}

func (s *formFactorTestService) DeviceProfiles(
	_ context.Context,
	_ string,
) ([]formfactordomain.DeviceProfile, error) {
	profiles := make([]formfactordomain.DeviceProfile, 0, len(s.profiles))
	for _, profile := range s.profiles {
		profiles = append(profiles, formfactordomain.DeviceProfile{
			DeviceID:       profile.DeviceID,
			TechnicalName:  profile.TechnicalName,
			CapabilityMask: profile.CapabilityMask,
			ActionMask:     profile.ActionMask,
			Capabilities:   formfactordomain.Capabilities(profile.CapabilityMask),
			Actions:        formfactordomain.Actions(profile.ActionMask),
			SchemaVersion:  profile.SchemaVersion,
		})
	}
	return profiles, nil
}

func (s *formFactorTestService) AdminStats(
	_ context.Context,
	windowDays int,
) (formfactordomain.AdminStats, error) {
	return formfactordomain.AdminStats{
		WindowDays:  windowDays,
		DeviceCount: uint64(len(s.profiles)),
	}, nil
}

func TestFormFactorCapabilityRequiresAuthentication(t *testing.T) {
	handler := formFactorHandler{service: &formFactorTestService{}}
	request := englishRequest(
		http.MethodGet,
		"/api/v1/devices/device-1/form-factor/capability",
		"",
		"",
	)
	request.SetPathValue("device_id", "device-1")
	recorder := httptest.NewRecorder()
	handler.capability(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestFormFactorCapabilityComposesActions(t *testing.T) {
	service := &formFactorTestService{
		profiles: map[string]formfactordomain.Profile{
			"device-1": {
				DeviceID:       "device-1",
				TechnicalName:  "moving_body",
				CapabilityMask: formfactordomain.CapLocomotion | formfactordomain.CapServo,
				ActionMask: formfactordomain.ComposeActions(
					formfactordomain.CapLocomotion | formfactordomain.CapServo,
				),
				SchemaVersion: formfactordomain.SchemaVersion,
			},
		},
	}
	handler := formFactorHandler{service: service}
	request := englishRequest(
		http.MethodGet,
		"/api/v1/devices/device-1/form-factor/capability",
		"",
		"family-1",
	)
	request.SetPathValue("device_id", "device-1")
	recorder := httptest.NewRecorder()
	handler.capability(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var envelope struct {
		Data formfactordomain.CapabilitySummary `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !envelope.Data.Supported ||
		envelope.Data.ActionMask&formfactordomain.ActionWalk == 0 ||
		envelope.Data.ActionMask&formfactordomain.ActionTail == 0 {
		t.Fatalf("unexpected capability: %+v", envelope.Data)
	}
}
