package app

import (
	assessmentService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/assessment/service"
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
