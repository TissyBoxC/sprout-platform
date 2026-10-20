// Command device-platform starts the 芽系列·初芽 device management service.
package main

import (
	"fmt"
	"os"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/app"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/buildinfo"
)

// buildVersion is set with -X at release build time.
var buildVersion = buildinfo.Version

func main() {
	buildinfo.Version = buildVersion
	if err := app.Run(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
