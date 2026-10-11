package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	companionshipdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/companionship/domain"
	companionshipservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/companionship/service"
)

// companionshipTestRepository is a tiny in-memory double so the handler tests
// exercise the real service validation without a database.
type companionshipTestRepository struct {
	records   map[string]*companionshipdomain.GrowthRecord
	reminders map[string]*companionshipdomain.HabitReminder
}

func newCompanionshipTestRepository() *companionshipTestRepository {
	return &companionshipTestRepository{
		records:   map[string]*companionshipdomain.GrowthRecord{},
		reminders: map[string]*companionshipdomain.HabitReminder{},
	}
}

func (r *companionshipTestRepository) CreateGrowthRecord(
	_ context.Context,
	record *companionshipdomain.GrowthRecord,
) error {
	copyRecord := *record
	r.records[record.ID] = &copyRecord
	return nil
}

func (r *companionshipTestRepository) ListGrowthRecords(
	_ context.Context,
	familyID string,
	query companionshipdomain.GrowthRecordQuery,
) ([]companionshipdomain.GrowthRecord, error) {
	result := make([]companionshipdomain.GrowthRecord, 0, len(r.records))
	for _, record := range r.records {
		if record.FamilyID != familyID {
			continue
		}
		if query.ChildID != "" && record.ChildID != query.ChildID {
			continue
		}
		if query.Category != "" && record.Category != query.Category {
			continue
		}
		result = append(result, *record)
	}
	if query.Limit > 0 && len(result) > query.Limit {
		result = result[:query.Limit]
	}
	return result, nil
}

func (r *companionshipTestRepository) GetGrowthRecord(
	_ context.Context,
	familyID string,
	recordID string,
) (*companionshipdomain.GrowthRecord, error) {
	record, ok := r.records[recordID]
	if !ok || record.FamilyID != familyID {
		return nil, companionshipdomain.ErrGrowthRecordNotFound
	}
	copyRecord := *record
	return &copyRecord, nil
}

func (r *companionshipTestRepository) DeleteGrowthRecord(
	_ context.Context,
	familyID string,
	recordID string,
) error {
	record, ok := r.records[recordID]
	if !ok || record.FamilyID != familyID {
		return companionshipdomain.ErrGrowthRecordNotFound
	}
	delete(r.records, recordID)
	return nil
}

func (r *companionshipTestRepository) CreateHabitReminder(
	_ context.Context,
	reminder *companionshipdomain.HabitReminder,
) error {
	copyReminder := *reminder
	r.reminders[reminder.ID] = &copyReminder
	return nil
}

func (r *companionshipTestRepository) UpdateHabitReminder(
	_ context.Context,
	reminder *companionshipdomain.HabitReminder,
) error {
	existing, ok := r.reminders[reminder.ID]
	if !ok || existing.FamilyID != reminder.FamilyID {
		return companionshipdomain.ErrHabitReminderNotFound
	}
	copyReminder := *reminder
	r.reminders[reminder.ID] = &copyReminder
	return nil
}

func (r *companionshipTestRepository) ListHabitReminders(
	_ context.Context,
	familyID string,
) ([]companionshipdomain.HabitReminder, error) {
	result := make([]companionshipdomain.HabitReminder, 0, len(r.reminders))
	for _, reminder := range r.reminders {
		if reminder.FamilyID != familyID {
			continue
		}
		result = append(result, *reminder)
	}
	return result, nil
}

func (r *companionshipTestRepository) GetHabitReminder(
	_ context.Context,
	familyID string,
	reminderID string,
) (*companionshipdomain.HabitReminder, error) {
	reminder, ok := r.reminders[reminderID]
	if !ok || reminder.FamilyID != familyID {
		return nil, companionshipdomain.ErrHabitReminderNotFound
	}
	copyReminder := *reminder
	return &copyReminder, nil
}

func (r *companionshipTestRepository) DeleteHabitReminder(
	_ context.Context,
	familyID string,
	reminderID string,
) error {
	reminder, ok := r.reminders[reminderID]
	if !ok || reminder.FamilyID != familyID {
		return companionshipdomain.ErrHabitReminderNotFound
	}
	delete(r.reminders, reminderID)
	return nil
}

func newCompanionshipHandlerForTest(t *testing.T) companionshipHandler {
	t.Helper()
	service, err := companionshipservice.New(companionshipservice.Options{
		Repository: newCompanionshipTestRepository(),
	})
	if err != nil {
		t.Fatalf("create companionship service: %v", err)
	}
	return companionshipHandler{service: service}
}

func companionshipRequestWithActor(
	method string,
	target string,
	body string,
	accountID string,
) *http.Request {
	var request *http.Request
	if body == "" {
		request = httptest.NewRequest(method, target, nil)
	} else {
		request = httptest.NewRequest(method, target, strings.NewReader(body))
	}
	if accountID == "" {
		return request
	}
	return request.WithContext(context.WithValue(
		request.Context(),
		authenticatedAccountKey{},
		accountID,
	))
}

func TestCompanionshipGrowthRecordRequiresAuthentication(t *testing.T) {
	handler := newCompanionshipHandlerForTest(t)
	request := companionshipRequestWithActor(
		http.MethodGet,
		"/api/v1/companionship/growth-records",
		"",
		"",
	)
	recorder := httptest.NewRecorder()
	handler.listGrowthRecords(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestCompanionshipGrowthRecordLifecycleIsFamilyScoped(t *testing.T) {
	handler := newCompanionshipHandlerForTest(t)

	createRequest := companionshipRequestWithActor(
		http.MethodPost,
		"/api/v1/companionship/growth-records",
		`{"child_id":"child-1","category":"milestone","title":"第一次自己穿鞋"}`,
		"family-1",
	)
	createRecorder := httptest.NewRecorder()
	handler.createGrowthRecord(createRecorder, createRequest)
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d: %s",
			createRecorder.Code, http.StatusCreated, createRecorder.Body.String())
	}
	var created struct {
		Data struct {
			GrowthRecord struct {
				ID       string `json:"id"`
				Category string `json:"category"`
			} `json:"growth_record"`
		} `json:"data"`
	}
	if err := json.Unmarshal(createRecorder.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.Data.GrowthRecord.ID == "" ||
		created.Data.GrowthRecord.Category != "milestone" {
		t.Fatalf("unexpected create response: %s", createRecorder.Body.String())
	}

	// Another family cannot see the record.
	otherList := companionshipRequestWithActor(
		http.MethodGet,
		"/api/v1/companionship/growth-records",
		"",
		"family-2",
	)
	otherRecorder := httptest.NewRecorder()
	handler.listGrowthRecords(otherRecorder, otherList)
	if otherRecorder.Code != http.StatusOK {
		t.Fatalf("other list status = %d, want %d", otherRecorder.Code, http.StatusOK)
	}
	var otherEnvelope struct {
		Data struct {
			GrowthRecords []map[string]any `json:"growth_records"`
		} `json:"data"`
	}
	if err := json.Unmarshal(otherRecorder.Body.Bytes(), &otherEnvelope); err != nil {
		t.Fatalf("decode other list: %v", err)
	}
	if len(otherEnvelope.Data.GrowthRecords) != 0 {
		t.Fatalf("family-2 saw %d records, want 0", len(otherEnvelope.Data.GrowthRecords))
	}

	// Another family cannot delete the record either.
	foreignDelete := companionshipRequestWithActor(
		http.MethodDelete,
		"/api/v1/companionship/growth-records/x",
		"",
		"family-2",
	)
	foreignDelete.SetPathValue("record_id", created.Data.GrowthRecord.ID)
	foreignRecorder := httptest.NewRecorder()
	handler.deleteGrowthRecord(foreignRecorder, foreignDelete)
	if foreignRecorder.Code != http.StatusNotFound {
		t.Fatalf("foreign delete status = %d, want %d",
			foreignRecorder.Code, http.StatusNotFound)
	}

	deleteRequest := companionshipRequestWithActor(
		http.MethodDelete,
		"/api/v1/companionship/growth-records/x",
		"",
		"family-1",
	)
	deleteRequest.SetPathValue("record_id", created.Data.GrowthRecord.ID)
	deleteRecorder := httptest.NewRecorder()
	handler.deleteGrowthRecord(deleteRecorder, deleteRequest)
	if deleteRecorder.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want %d: %s",
			deleteRecorder.Code, http.StatusOK, deleteRecorder.Body.String())
	}
}

func TestCompanionshipGrowthRecordRejectsInvalidCategory(t *testing.T) {
	handler := newCompanionshipHandlerForTest(t)
	request := companionshipRequestWithActor(
		http.MethodPost,
		"/api/v1/companionship/growth-records",
		`{"category":"unknown","title":"标题"}`,
		"family-1",
	)
	recorder := httptest.NewRecorder()
	handler.createGrowthRecord(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusUnprocessableEntity, recorder.Body.String())
	}
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if envelope.Error.Code != "invalid_growth_category" {
		t.Fatalf("error code = %q, want invalid_growth_category", envelope.Error.Code)
	}
}

func TestCompanionshipGrowthRecordRejectsInvalidBefore(t *testing.T) {
	handler := newCompanionshipHandlerForTest(t)
	request := companionshipRequestWithActor(
		http.MethodGet,
		"/api/v1/companionship/growth-records?before=not-a-time",
		"",
		"family-1",
	)
	recorder := httptest.NewRecorder()
	handler.listGrowthRecords(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnprocessableEntity)
	}
}

func TestCompanionshipHabitReminderLifecycle(t *testing.T) {
	handler := newCompanionshipHandlerForTest(t)

	createRequest := companionshipRequestWithActor(
		http.MethodPost,
		"/api/v1/companionship/habit-reminders",
		`{"child_id":"child-1","label":"刷牙","prompt":"该刷牙啦","reminder_time":"07:30","weekdays":[1,3,5]}`,
		"family-1",
	)
	createRecorder := httptest.NewRecorder()
	handler.createHabitReminder(createRecorder, createRequest)
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d: %s",
			createRecorder.Code, http.StatusCreated, createRecorder.Body.String())
	}
	var created struct {
		Data struct {
			HabitReminder struct {
				ID       string `json:"id"`
				Enabled  bool   `json:"enabled"`
				Weekdays []int  `json:"weekdays"`
			} `json:"habit_reminder"`
		} `json:"data"`
	}
	if err := json.Unmarshal(createRecorder.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.Data.HabitReminder.ID == "" || !created.Data.HabitReminder.Enabled {
		t.Fatalf("unexpected create response: %s", createRecorder.Body.String())
	}
	if len(created.Data.HabitReminder.Weekdays) != 3 {
		t.Fatalf("weekdays = %v, want 3 entries", created.Data.HabitReminder.Weekdays)
	}

	updateRequest := companionshipRequestWithActor(
		http.MethodPut,
		"/api/v1/companionship/habit-reminders/x",
		`{"label":"刷牙","prompt":"该刷牙啦","reminder_time":"20:00","weekdays":[2,4,6],"enabled":false}`,
		"family-1",
	)
	updateRequest.SetPathValue("reminder_id", created.Data.HabitReminder.ID)
	updateRecorder := httptest.NewRecorder()
	handler.updateHabitReminder(updateRecorder, updateRequest)
	if updateRecorder.Code != http.StatusOK {
		t.Fatalf("update status = %d, want %d: %s",
			updateRecorder.Code, http.StatusOK, updateRecorder.Body.String())
	}
	var updated struct {
		Data struct {
			HabitReminder struct {
				Enabled bool `json:"enabled"`
			} `json:"habit_reminder"`
		} `json:"data"`
	}
	if err := json.Unmarshal(updateRecorder.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode update response: %v", err)
	}
	if updated.Data.HabitReminder.Enabled {
		t.Fatalf("expected reminder to be disabled after update")
	}

	listRequest := companionshipRequestWithActor(
		http.MethodGet,
		"/api/v1/companionship/habit-reminders",
		"",
		"family-1",
	)
	listRecorder := httptest.NewRecorder()
	handler.listHabitReminders(listRecorder, listRequest)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d", listRecorder.Code, http.StatusOK)
	}
	var listed struct {
		Data struct {
			HabitReminders []map[string]any `json:"habit_reminders"`
		} `json:"data"`
	}
	if err := json.Unmarshal(listRecorder.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listed.Data.HabitReminders) != 1 {
		t.Fatalf("listed %d reminders, want 1", len(listed.Data.HabitReminders))
	}

	deleteRequest := companionshipRequestWithActor(
		http.MethodDelete,
		"/api/v1/companionship/habit-reminders/x",
		"",
		"family-1",
	)
	deleteRequest.SetPathValue("reminder_id", created.Data.HabitReminder.ID)
	deleteRecorder := httptest.NewRecorder()
	handler.deleteHabitReminder(deleteRecorder, deleteRequest)
	if deleteRecorder.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want %d: %s",
			deleteRecorder.Code, http.StatusOK, deleteRecorder.Body.String())
	}
}

func TestCompanionshipHabitReminderRejectsInvalidTime(t *testing.T) {
	handler := newCompanionshipHandlerForTest(t)
	request := companionshipRequestWithActor(
		http.MethodPost,
		"/api/v1/companionship/habit-reminders",
		`{"label":"喝水","reminder_time":"25:99"}`,
		"family-1",
	)
	recorder := httptest.NewRecorder()
	handler.createHabitReminder(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusUnprocessableEntity, recorder.Body.String())
	}
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if envelope.Error.Code != "invalid_reminder_time" {
		t.Fatalf("error code = %q, want invalid_reminder_time", envelope.Error.Code)
	}
}

func TestCompanionshipHabitReminderEmptyWeekdaysExpandToAll(t *testing.T) {
	handler := newCompanionshipHandlerForTest(t)
	request := companionshipRequestWithActor(
		http.MethodPost,
		"/api/v1/companionship/habit-reminders",
		`{"label":"喝水","reminder_time":"09:00"}`,
		"family-1",
	)
	recorder := httptest.NewRecorder()
	handler.createHabitReminder(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	var created struct {
		Data struct {
			HabitReminder struct {
				Weekdays []int `json:"weekdays"`
			} `json:"habit_reminder"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if len(created.Data.HabitReminder.Weekdays) != 7 {
		t.Fatalf("weekdays = %v, want the full week", created.Data.HabitReminder.Weekdays)
	}
}

func TestCompanionshipServiceSatisfiesInterface(t *testing.T) {
	// Compile-time guard mirrored from the handler's own assertion so the
	// in-memory double above stays in sync with the production interface.
	var _ companionshipService = (*companionshipservice.Service)(nil)
}
