// Command voice-gateway starts the 芽系列·初芽 realtime voice gateway.
package main

import (
	"fmt"
	"os"

	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/app"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/buildinfo"
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
