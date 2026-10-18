package http

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	runtimeservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_runtime/service"
	weardomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/wearable/domain"
)

// wearableService is the guardian-facing wearable surface. The HTTP layer
// depends on this narrow interface so the module stays removable.
type wearableService interface {
	Policy(ctx context.Context, familyID string) (weardomain.LocationPolicy, error)
	UpdatePolicy(
		ctx context.Context,
		familyID string,
		input weardomain.LocationPolicy,
	) (weardomain.LocationPolicy, error)
	CreateContact(
		ctx context.Context,
		familyID string,
		input weardomain.ContactInput,
	) (weardomain.EmergencyContact, error)
	UpdateContact(
		ctx context.Context,
		familyID string,
		contactID string,
		input weardomain.ContactInput,
	) (weardomain.EmergencyContact, error)
	DeleteContact(ctx context.Context, familyID string, contactID string) error
	Contacts(ctx context.Context, familyID string) ([]weardomain.EmergencyContact, error)
	Geofence(ctx context.Context, familyID string) (weardomain.Geofence, bool, error)
	UpdateGeofence(
		ctx context.Context,
		familyID string,
		input weardomain.GeofenceInput,
	) (weardomain.Geofence, error)
	ReportLocation(
		ctx context.Context,
		familyID string,
		deviceID string,
		report weardomain.LocationReport,
	) (weardomain.DeviceStatus, error)
	DeviceStatuses(ctx context.Context, familyID string) ([]weardomain.DeviceStatusView, error)
	Capability(
		ctx context.Context,
		familyID string,
		deviceID string,
	) (weardomain.DeviceStatusView, error)
	TriggerSOS(
		ctx context.Context,
		familyID string,
		input weardomain.SOSInput,
	) (weardomain.SOSEvent, error)
	CancelSOS(ctx context.Context, familyID string, eventID string) (weardomain.SOSEvent, error)
	SOSHistory(ctx context.Context, familyID string, limit int) ([]weardomain.SOSEvent, error)
	ActiveSOS(ctx context.Context, familyID string) ([]weardomain.SOSEvent, error)
	AdminStats(ctx context.Context, windowDays int) (weardomain.AdminStats, error)
}

type wearableHandler struct {
	service wearableService
}

// deviceWearableHandler records bounded location and SOS reports from a bound
// device.
type deviceWearableHandler struct {
	handler        *wearableHandler
	bindingService englishSessionResolver
	runtimeService *runtimeservice.Service
}

type deviceWearablePolicyResponse struct {
	SchemaVersion         int    `json:"schema_version"`
	Enabled               bool   `json:"enabled"`
	PermissionGranted     bool   `json:"permission_granted"`
	Precision             string `json:"precision"`
	UploadIntervalSeconds int    `json:"upload_interval_seconds"`
	RetentionDays         int    `json:"retention_days"`
	GeofenceRadiusMeters  int    `json:"geofence_radius_meters"`
}

type wearablePolicyPayload struct {
	Enabled               bool   `json:"enabled"`
	PermissionGranted     bool   `json:"permission_granted"`
	Precision             string `json:"precision"`
	UploadIntervalSeconds int    `json:"upload_interval_seconds"`
	RetentionDays         int    `json:"retention_days"`
	GeofenceRadiusMeters  int    `json:"geofence_radius_meters"`
}

type wearableContactPayload struct {
	ChildID     string `json:"child_id"`
	DisplayName string `json:"display_name"`
	Phone       string `json:"phone"`
	Role        string `json:"role"`
	Priority    int    `json:"priority"`
	Enabled     bool   `json:"enabled"`
}

type wearableGeofencePayload struct {
	ChildID      string  `json:"child_id"`
	Latitude     float64 `json:"latitude"`
	Longitude    float64 `json:"longitude"`
	RadiusMeters int     `json:"radius_meters"`
	Enabled      bool    `json:"enabled"`
}

type wearableLocationPayload struct {
	Latitude       float64 `json:"latitude"`
	Longitude      float64 `json:"longitude"`
	AccuracyMeters float64 `json:"accuracy_meters"`
	Precision      string  `json:"precision"`
	GeofenceState  string  `json:"geofence_state"`
	SampledAt      string  `json:"sampled_at"`
}

type wearableSOSPayload struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// policy returns the family location policy, or the conservative default.
func (handler wearableHandler) policy(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedFamilyOwnerID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	policy, err := handler.service.Policy(request.Context(), familyID)
	if err != nil {
		writeWearableError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, wearablePolicyResponse(policy))
}

// updatePolicy stores the family location policy.
func (handler wearableHandler) updatePolicy(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedFamilyOwnerID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	var payload wearablePolicyPayload
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查定位设置")
		return
	}
	policy, err := handler.service.UpdatePolicy(request.Context(), familyID, weardomain.LocationPolicy{
		Enabled:               payload.Enabled,
		PermissionGranted:     payload.PermissionGranted,
		Precision:             payload.Precision,
		UploadIntervalSeconds: payload.UploadIntervalSeconds,
		RetentionDays:         payload.RetentionDays,
		GeofenceRadiusMeters:  payload.GeofenceRadiusMeters,
	})
	if err != nil {
		writeWearableError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, wearablePolicyResponse(policy))
}

// listContacts returns every emergency contact in the authenticated family.
func (handler wearableHandler) listContacts(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedFamilyOwnerID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	contacts, err := handler.service.Contacts(request.Context(), familyID)
	if err != nil {
		writeWearableError(response, request, err)
		return
	}
	if contacts == nil {
		contacts = []weardomain.EmergencyContact{}
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"contacts": wearableContactResponses(contacts),
	})
}

// createContact stores a new emergency contact.
func (handler wearableHandler) createContact(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedFamilyOwnerID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	var payload wearableContactPayload
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查联系人信息")
		return
	}
	contact, err := handler.service.CreateContact(request.Context(), familyID, weardomain.ContactInput{
		ChildID:     payload.ChildID,
		DisplayName: payload.DisplayName,
		Phone:       payload.Phone,
		Role:        payload.Role,
		Priority:    payload.Priority,
		Enabled:     payload.Enabled,
	})
	if err != nil {
		writeWearableError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusCreated, wearableContactResponse(contact))
}

// updateContact stores one emergency contact edit.
func (handler wearableHandler) updateContact(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedFamilyOwnerID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	contactID := strings.TrimSpace(request.PathValue("contact_id"))
	var payload wearableContactPayload
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查联系人信息")
		return
	}
	contact, err := handler.service.UpdateContact(
		request.Context(),
		familyID,
		contactID,
		weardomain.ContactInput{
			ChildID:     payload.ChildID,
			DisplayName: payload.DisplayName,
			Phone:       payload.Phone,
			Role:        payload.Role,
			Priority:    payload.Priority,
			Enabled:     payload.Enabled,
		},
	)
	if err != nil {
		writeWearableError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, wearableContactResponse(contact))
}

// deleteContact removes one emergency contact.
func (handler wearableHandler) deleteContact(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedFamilyOwnerID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	contactID := strings.TrimSpace(request.PathValue("contact_id"))
	if err := handler.service.DeleteContact(request.Context(), familyID, contactID); err != nil {
		writeWearableError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{"deleted": true})
}

// geofence returns the family home geofence.
func (handler wearableHandler) geofence(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedFamilyOwnerID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	geofence, found, err := handler.service.Geofence(request.Context(), familyID)
	if err != nil {
		writeWearableError(response, request, err)
		return
	}
	if !found {
		writeSuccess(response, request, http.StatusOK, map[string]any{"geofence": nil})
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"geofence": wearableGeofenceResponse(geofence),
	})
}

// updateGeofence stores the family home geofence.
func (handler wearableHandler) updateGeofence(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedFamilyOwnerID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	var payload wearableGeofencePayload
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查电子围栏设置")
		return
	}
	geofence, err := handler.service.UpdateGeofence(request.Context(), familyID, weardomain.GeofenceInput{
		ChildID:      payload.ChildID,
		Latitude:     payload.Latitude,
		Longitude:    payload.Longitude,
		RadiusMeters: payload.RadiusMeters,
		Enabled:      payload.Enabled,
	})
	if err != nil {
		writeWearableError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, wearableGeofenceResponse(geofence))
}

// statuses returns each family device's latest wearable status.
func (handler wearableHandler) statuses(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedFamilyOwnerID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	devices, err := handler.service.DeviceStatuses(request.Context(), familyID)
	if err != nil {
		writeWearableError(response, request, err)
		return
	}
	if devices == nil {
		devices = []weardomain.DeviceStatusView{}
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{"devices": devices})
}

// capability returns the wearable capability for one family device.
func (handler wearableHandler) capability(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedFamilyOwnerID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	deviceID := strings.TrimSpace(request.PathValue("device_id"))
	view, err := handler.service.Capability(request.Context(), familyID, deviceID)
	if err != nil {
		writeWearableError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, view)
}

// sosHistory returns the recent SOS history in the family.
func (handler wearableHandler) sosHistory(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedFamilyOwnerID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	limit := parseWearableLimit(request.URL.Query().Get("limit"))
	events, err := handler.service.SOSHistory(request.Context(), familyID, limit)
	if err != nil {
		writeWearableError(response, request, err)
		return
	}
	if events == nil {
		events = []weardomain.SOSEvent{}
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"events": wearableSOSResponses(events),
	})
}

// activeSOS returns every active SOS in the family.
func (handler wearableHandler) activeSOS(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedFamilyOwnerID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	events, err := handler.service.ActiveSOS(request.Context(), familyID)
	if err != nil {
		writeWearableError(response, request, err)
		return
	}
	if events == nil {
		events = []weardomain.SOSEvent{}
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"events": wearableSOSResponses(events),
	})
}

// cancelSOS cancels one active SOS event.
func (handler wearableHandler) cancelSOS(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedFamilyOwnerID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	eventID := strings.TrimSpace(request.PathValue("event_id"))
	event, err := handler.service.CancelSOS(request.Context(), familyID, eventID)
	if err != nil {
		writeWearableError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, wearableSOSResponse(event))
}

// adminStats returns the bounded fleet wearable summary.
func (handler wearableHandler) adminStats(
	response http.ResponseWriter,
	request *http.Request,
) {
	windowDays := parseVideoWindowDays(request.URL.Query().Get("window_days"))
	stats, err := handler.service.AdminStats(request.Context(), windowDays)
	if err != nil {
		writeWearableError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, stats)
}

// reportLocation records one bound device's bounded position.
func (handler deviceWearableHandler) reportLocation(
	response http.ResponseWriter,
	request *http.Request,
) {
	deviceID, familyID, ok := handler.verifyDevice(response, request)
	if !ok {
		return
	}
	var payload wearableLocationPayload
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查上报内容")
		return
	}
	status, err := handler.handler.service.ReportLocation(
		request.Context(),
		familyID,
		deviceID,
		weardomain.LocationReport{
			Latitude:       payload.Latitude,
			Longitude:      payload.Longitude,
			AccuracyMeters: payload.AccuracyMeters,
			Precision:      payload.Precision,
			GeofenceState:  payload.GeofenceState,
		},
	)
	if err != nil {
		writeWearableError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"device_id":      status.DeviceID,
		"geofence_state": status.GeofenceState,
		"recorded":       true,
	})
}

// devicePolicy returns the family location policy to a bound device that holds
// a device session token, so sampling and upload honour the guardian choice.
func (handler deviceWearableHandler) devicePolicy(
	response http.ResponseWriter,
	request *http.Request,
) {
	_, familyID, ok := handler.verifyDevice(response, request)
	if !ok {
		return
	}
	policy, err := handler.handler.service.Policy(request.Context(), familyID)
	if err != nil {
		writeWearableError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, deviceWearablePolicyResponse{
		SchemaVersion:         weardomain.SchemaVersion,
		Enabled:               policy.Enabled,
		PermissionGranted:     policy.PermissionGranted,
		Precision:             policy.Precision,
		UploadIntervalSeconds: policy.UploadIntervalSeconds,
		RetentionDays:         policy.RetentionDays,
		GeofenceRadiusMeters:  policy.GeofenceRadiusMeters,
	})
}

// reportSOS records one bound device's SOS trigger.
func (handler deviceWearableHandler) reportSOS(
	response http.ResponseWriter,
	request *http.Request,
) {
	deviceID, familyID, ok := handler.verifyDevice(response, request)
	if !ok {
		return
	}
	var payload wearableSOSPayload
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查上报内容")
		return
	}
	event, err := handler.handler.service.TriggerSOS(
		request.Context(),
		familyID,
		weardomain.SOSInput{
			DeviceID:  deviceID,
			Latitude:  payload.Latitude,
			Longitude: payload.Longitude,
		},
	)
	if err != nil {
		writeWearableError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusCreated, wearableSOSResponse(event))
}

func (handler deviceWearableHandler) verifyDevice(
	response http.ResponseWriter,
	request *http.Request,
) (string, string, bool) {
	if handler.bindingService == nil || handler.runtimeService == nil {
		writeError(response, request, http.StatusUnauthorized, "device_session_expired", "设备登录已过期，请重新连接")
		return "", "", false
	}
	deviceSessionToken, ok := bearerToken(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "device_session_expired", "设备登录已过期，请重新连接")
		return "", "", false
	}
	pathDeviceID := strings.TrimSpace(request.PathValue("device_id"))
	sessionDeviceID, err := handler.bindingService.VerifyDeviceSession(
		request.Context(),
		deviceSessionToken,
	)
	if err != nil || sessionDeviceID != pathDeviceID {
		writeError(response, request, http.StatusUnauthorized, "device_session_expired", "设备登录已过期，请重新连接")
		return "", "", false
	}
	familyID, err := handler.runtimeService.ResolveDeviceFamily(
		request.Context(),
		deviceSessionToken,
		pathDeviceID,
	)
	if err != nil {
		writeWearableError(response, request, err)
		return "", "", false
	}
	return pathDeviceID, familyID, true
}

func wearablePolicyResponse(policy weardomain.LocationPolicy) map[string]any {
	return map[string]any{
		"enabled":                 policy.Enabled,
		"permission_granted":      policy.PermissionGranted,
		"precision":               policy.Precision,
		"upload_interval_seconds": policy.UploadIntervalSeconds,
		"retention_days":          policy.RetentionDays,
		"geofence_radius_meters":  policy.GeofenceRadiusMeters,
		"updated_at":              policy.UpdatedAt,
	}
}

func wearableContactResponse(contact weardomain.EmergencyContact) map[string]any {
	return map[string]any{
		"id":           contact.ID,
		"child_id":     contact.ChildID,
		"display_name": contact.DisplayName,
		"phone":        contact.Phone,
		"role":         contact.Role,
		"priority":     contact.Priority,
		"enabled":      contact.Enabled,
		"created_at":   contact.CreatedAt,
	}
}

func wearableContactResponses(
	contacts []weardomain.EmergencyContact,
) []map[string]any {
	results := make([]map[string]any, 0, len(contacts))
	for _, contact := range contacts {
		results = append(results, wearableContactResponse(contact))
	}
	return results
}

func wearableGeofenceResponse(geofence weardomain.Geofence) map[string]any {
	return map[string]any{
		"id":            geofence.ID,
		"child_id":      geofence.ChildID,
		"latitude":      geofence.Latitude,
		"longitude":     geofence.Longitude,
		"radius_meters": geofence.RadiusMeters,
		"enabled":       geofence.Enabled,
		"updated_at":    geofence.UpdatedAt,
	}
}

func wearableSOSResponse(event weardomain.SOSEvent) map[string]any {
	response := map[string]any{
		"id":           event.ID,
		"device_id":    event.DeviceID,
		"latitude":     event.Latitude,
		"longitude":    event.Longitude,
		"state":        event.State,
		"triggered_at": event.TriggeredAt,
	}
	if event.CancelledAt != nil {
		response["cancelled_at"] = event.CancelledAt
	}
	return response
}

func wearableSOSResponses(events []weardomain.SOSEvent) []map[string]any {
	results := make([]map[string]any, 0, len(events))
	for _, event := range events {
		results = append(results, wearableSOSResponse(event))
	}
	return results
}

func parseWearableLimit(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 || value > weardomain.SOSHistoryMaxLimit {
		return 0
	}
	return value
}

func writeWearableError(
	response http.ResponseWriter,
	request *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, weardomain.ErrInvalidPolicy):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_policy", "请检查定位设置")
	case errors.Is(err, weardomain.ErrInvalidLocation):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_location", "请检查定位上报")
	case errors.Is(err, weardomain.ErrLocationNotFound):
		writeError(response, request, http.StatusNotFound, "location_not_found", "定位不存在")
	case errors.Is(err, weardomain.ErrInvalidContact):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_contact", "请检查联系人信息")
	case errors.Is(err, weardomain.ErrContactNotFound):
		writeError(response, request, http.StatusNotFound, "contact_not_found", "联系人不存在")
	case errors.Is(err, weardomain.ErrInvalidGeofence):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_geofence", "请检查电子围栏设置")
	case errors.Is(err, weardomain.ErrGeofenceNotFound):
		writeError(response, request, http.StatusNotFound, "geofence_not_found", "电子围栏不存在")
	case errors.Is(err, weardomain.ErrInvalidSOS):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_sos", "请检查紧急求助")
	case errors.Is(err, weardomain.ErrSOSNotFound):
		writeError(response, request, http.StatusNotFound, "sos_not_found", "紧急求助记录不存在")
	default:
		writeError(response, request, http.StatusInternalServerError, "service_error", "操作没有完成，请稍后重试")
	}
}
