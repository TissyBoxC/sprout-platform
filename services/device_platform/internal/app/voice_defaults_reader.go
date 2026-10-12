// Package app wires platform modules together.
package app

import (
	"context"

	operationsdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/operations/domain"
	policyService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/parent_policy/service"
)

// operationsVoiceDefaults adapts the operations settings document to the
// narrow voice-default contract consumed by parent policy provisioning.
type operationsVoiceDefaults struct {
	operations interface {
		VoiceDefaults(context.Context) (*operationsdomain.VoiceSettings, error)
	}
}

// VoiceDefaults implements policyService.VoiceDefaultsReader.
func (reader operationsVoiceDefaults) VoiceDefaults(
	ctx context.Context,
) (policyService.VoiceDefaults, error) {
	voice, err := reader.operations.VoiceDefaults(ctx)
	if err != nil {
		return policyService.VoiceDefaults{}, err
	}
	if voice == nil {
		return policyService.VoiceDefaults{}, nil
	}
	return policyService.VoiceDefaults{
		ContinuousConversationEnabled: voice.ContinuousConversationEnabled,
		IdleWindowSeconds:             voice.IdleWindowSeconds,
		BargeInEnabled:                voice.BargeInEnabled,
		FarFieldEnabled:               voice.FarFieldEnabled,
	}, nil
}
