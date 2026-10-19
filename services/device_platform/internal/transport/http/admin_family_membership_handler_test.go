package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	familydomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/family/domain"
)

func TestAdminMembershipListReturnsRevisionAndSafeIdentity(t *testing.T) {
	_, service := newFamilyHandlerForTest()
	handler := adminFamilyMembershipHandler{service: service}
	request := englishRequest(
		http.MethodGet,
		"/api/v1/admin/families/family-1/members",
		"",
		"admin-1",
	)
	request.SetPathValue("parent_account_id", "family-1")
	recorder := httptest.NewRecorder()

	handler.listMembers(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var envelope struct {
		Data struct {
			FamilyID           string `json:"family_id"`
			MembershipRevision int64  `json:"membership_revision"`
			Members            []struct {
				UserID        string `json:"user_id"`
				DisplayName   string `json:"display_name"`
				AccountRole   string `json:"account_role"`
				AccountStatus string `json:"account_status"`
			} `json:"members"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data.FamilyID != "family-1" ||
		envelope.Data.MembershipRevision != 1 ||
		len(envelope.Data.Members) != 1 {
		t.Fatalf("unexpected list response: %s", recorder.Body.String())
	}
	member := envelope.Data.Members[0]
	if member.AccountRole != "parent" || member.AccountStatus != "active" {
		t.Fatalf("safe identity missing: %+v", member)
	}
	for _, forbidden := range []string{"phone", "email", "password"} {
		if strings.Contains(recorder.Body.String(), `"`+forbidden+`"`) {
			t.Fatalf("admin membership response leaked %s: %s", forbidden, recorder.Body.String())
		}
	}
}

func TestAdminMembershipWritesRequireRevision(t *testing.T) {
	_, service := newFamilyHandlerForTest()
	handler := adminFamilyMembershipHandler{service: service}
	request := englishRequest(
		http.MethodPost,
		"/api/v1/admin/families/family-1/invitations",
		`{"email":"guardian@example.com","role":"guardian"}`,
		"admin-1",
	)
	request.SetPathValue("parent_account_id", "family-1")
	recorder := httptest.NewRecorder()

	handler.createInvitation(recorder, request)

	if recorder.Code != http.StatusPreconditionRequired {
		t.Fatalf("status = %d, want 428: %s", recorder.Code, recorder.Body.String())
	}
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Error.Code != "revision_required" {
		t.Fatalf("error code = %q", envelope.Error.Code)
	}
}

func TestAdminMembershipRoleUsesIfMatchRevision(t *testing.T) {
	_, service := newFamilyHandlerForTest()
	service.members["member-1"] = familydomain.Member{
		ID:       "member-row-1",
		FamilyID: "family-1",
		UserID:   "member-1",
		Role:     familydomain.RoleViewer,
	}
	handler := adminFamilyMembershipHandler{service: service}
	request := englishRequest(
		http.MethodPut,
		"/api/v1/admin/families/family-1/members/member-1",
		`{"role":"guardian"}`,
		"admin-1",
	)
	request.SetPathValue("parent_account_id", "family-1")
	request.SetPathValue("user_id", "member-1")
	request.Header.Set("If-Match", `"1"`)
	recorder := httptest.NewRecorder()

	handler.updateMemberRole(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
}

func TestAdminMembershipRemoveMemberAcceptsEmptyBody(t *testing.T) {
	_, service := newFamilyHandlerForTest()
	service.members["member-1"] = familydomain.Member{
		ID:       "member-row-1",
		FamilyID: "family-1",
		UserID:   "member-1",
		Role:     familydomain.RoleViewer,
	}
	handler := adminFamilyMembershipHandler{service: service}
	request := englishRequest(
		http.MethodDelete,
		"/api/v1/admin/families/family-1/members/member-1",
		"",
		"admin-1",
	)
	request.SetPathValue("parent_account_id", "family-1")
	request.SetPathValue("user_id", "member-1")
	request.Header.Set("If-Match", `"1"`)
	recorder := httptest.NewRecorder()

	handler.removeMember(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
}

type conflictAdminFamilyService struct {
	*familyTestService
}

func (conflictAdminFamilyService) AdminUpdateMemberRole(
	_ context.Context,
	_ string,
	_ string,
	_ string,
	_ string,
	_ int64,
) (familydomain.Member, error) {
	return familydomain.Member{}, familydomain.ErrRevisionConflict
}

func TestAdminMembershipRevisionConflictMapsToStable409(t *testing.T) {
	handler := adminFamilyMembershipHandler{
		service: conflictAdminFamilyService{newFamilyTestService()},
	}
	request := englishRequest(
		http.MethodPut,
		"/api/v1/admin/families/family-1/members/member-1",
		`{"role":"guardian"}`,
		"admin-1",
	)
	request.SetPathValue("parent_account_id", "family-1")
	request.SetPathValue("user_id", "member-1")
	request.Header.Set("If-Match", `"2"`)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.updateMemberRole(recorder, request)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", recorder.Code, recorder.Body.String())
	}
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Error.Code != "membership_revision_conflict" {
		t.Fatalf("error code = %q, want membership_revision_conflict", envelope.Error.Code)
	}
}
