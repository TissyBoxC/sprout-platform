package http

import (
	"testing"

	runtimedomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_runtime/domain"
)

func TestDeviceStatusResponsePublishesManagementCapability(t *testing.T) {
	status := &runtimedomain.DeviceStatus{DeviceID: "device-001"}

	viewer := deviceStatusResponseWithPermission(status, false)
	if viewer["can_manage"] != false {
		t.Fatalf("viewer can_manage = %#v, want false", viewer["can_manage"])
	}

	manager := deviceStatusResponseWithPermission(status, true)
	if manager["can_manage"] != true {
		t.Fatalf("manager can_manage = %#v, want true", manager["can_manage"])
	}
}
