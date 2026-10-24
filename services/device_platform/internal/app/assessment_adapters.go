package app

import (
	"context"
	"time"

	assessmentDomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/assessment/domain"
	assessmentService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/assessment/service"
	runtimeDomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_runtime/domain"
	englishService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/english/service"
)

// newAssessmentCatalog narrows the English learning service to the two catalog
// reads an assessment needs and adapts them to the assessment course seam.
//
// The adapter lives in this composition root so neither module imports the
// other: the assessment module owns its Item vocabulary and the English module
// keeps owning the lesson catalog.
func newAssessmentCatalog(english *englishService.Service) assessmentService.LessonCatalog {
	if english == nil {
		return nil
	}
	return assessmentService.NewEnglishCatalog(english)
}

// assessmentNotifier turns a guardian's "start this assessment on that device"
// into a device command.
//
// It carries only the session identifier and the deadline. The lesson, the item
// text and every answer stay behind the device's own authenticated session, so a
// command captured from the broker reveals no course content and no child data.
type assessmentNotifier struct {
	commands interface {
		CreateCommand(
			ctx context.Context,
			deviceID string,
			commandType runtimeDomain.CommandType,
			requestedBy string,
			payload map[string]any,
		) (*runtimeDomain.Command, error)
	}
}

// NotifyAssessmentStarted asks the named device to open the assessment.
func (notifier assessmentNotifier) NotifyAssessmentStarted(
	ctx context.Context,
	deviceID string,
	session assessmentDomain.Session,
) error {
	if notifier.commands == nil {
		return nil
	}
	payload := map[string]any{
		"session_id": session.ID,
		// The device enforces this deadline itself, so an assessment left open on
		// the screen cannot outlive the platform's own expiry.
		"expires_at": session.ExpiresAt.Format(time.RFC3339),
	}
	_, err := notifier.commands.CreateCommand(
		ctx,
		deviceID,
		runtimeDomain.CommandAssessmentStart,
		"guardian",
		payload,
	)
	return err
}
