package app

import (
	"context"
	"sort"
	"strconv"
	"strings"

	aiDomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ai_gateway/domain"
	aiService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ai_gateway/service"
	featureCenterService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/feature_center/service"
)

// aiModelCatalogReader adapts the AI gateway's runtime configuration to the
// feature center's model-option contract. The gateway is the single source of
// truth for which models exist; the console never hard-codes a model list.
type aiModelCatalogReader struct {
	service *aiService.Service
}

// ModelOptions returns the available models ordered by ascending latency so the
// fastest model is easy to pick. Models without a measurement sort last.
func (reader aiModelCatalogReader) ModelOptions(
	ctx context.Context,
) ([]featureCenterService.ModelOption, error) {
	if reader.service == nil {
		return nil, nil
	}
	config, err := reader.service.RuntimeConfigForAdmin(ctx)
	if err != nil {
		return nil, err
	}
	options := make([]featureCenterService.ModelOption, 0, len(config.Models))
	for _, model := range config.Models {
		id := strings.TrimSpace(model.Model)
		if id == "" {
			continue
		}
		options = append(options, featureCenterService.ModelOption{
			ID:    id,
			Label: modelLabel(model),
		})
	}
	sort.SliceStable(options, func(left int, right int) bool {
		leftLatency, leftKnown := modelLatency(config.Models, options[left].ID)
		rightLatency, rightKnown := modelLatency(config.Models, options[right].ID)
		if leftKnown != rightKnown {
			return leftKnown
		}
		if leftKnown && rightKnown && leftLatency != rightLatency {
			return leftLatency < rightLatency
		}
		return options[left].ID < options[right].ID
	})
	return options, nil
}

func modelLabel(model aiDomain.ProviderModelLatency) string {
	label := strings.TrimSpace(model.Model)
	if latency, ok := modelLatencyValue(model); ok {
		return label + "（约 " + latency + " ms）"
	}
	return label
}

func modelLatency(
	models []aiDomain.ProviderModelLatency,
	id string,
) (int, bool) {
	for _, model := range models {
		if strings.TrimSpace(model.Model) != id {
			continue
		}
		if model.PrimaryLatencyMs != nil {
			return *model.PrimaryLatencyMs, true
		}
		if model.AverageLatency7DaysMs != nil {
			return *model.AverageLatency7DaysMs, true
		}
		return 0, false
	}
	return 0, false
}

func modelLatencyValue(model aiDomain.ProviderModelLatency) (string, bool) {
	if model.PrimaryLatencyMs != nil {
		return strconv.Itoa(*model.PrimaryLatencyMs), true
	}
	if model.AverageLatency7DaysMs != nil {
		return strconv.Itoa(*model.AverageLatency7DaysMs), true
	}
	return "", false
}
