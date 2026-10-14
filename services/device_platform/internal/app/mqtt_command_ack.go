package app

import (
	"context"
	"errors"
	"fmt"

	runtimedomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_runtime/domain"
	runtimeservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_runtime/service"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/transport/mqtt"
)

// mqttCommandAckAdapter maps MQTT command acknowledgements onto the device
// runtime service. The device id passed in always comes from the authenticated
// topic, never from the payload.
type mqttCommandAckAdapter struct {
	runtime *runtimeservice.Service
}

func (adapter mqttCommandAckAdapter) HandleCommandAck(
	ctx context.Context,
	deviceID string,
	ack mqtt.DeviceCommandAck,
) error {
	if adapter.runtime == nil {
		return errors.New("device runtime service is required")
	}
	status := runtimedomain.CommandStatus(ack.Payload.Status)
	if status != runtimedomain.CommandStatusAcknowledged &&
		status != runtimedomain.CommandStatusFailed {
		return fmt.Errorf("unsupported command ack status %q", ack.Payload.Status)
	}
	return adapter.runtime.AcknowledgeCommandFromDevice(
		ctx,
		deviceID,
		ack.Payload.CommandID,
		status,
		ack.Payload.ResultCode,
	)
}
