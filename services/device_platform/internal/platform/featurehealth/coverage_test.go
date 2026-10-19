package featurehealth_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestCapabilityServicesExposeReadiness prevents a capability from being
// registered in the Feature Center while silently falling back to a schema-only
// health signal. This is a source contract, not a second runtime implementation.
func TestCapabilityServicesExposeReadiness(t *testing.T) {
	root := capabilityModulesRoot(t)
	modules := []string{
		"telemetry",
		"vision",
		"display",
		"input",
		"screen",
		"connectivity",
		"power",
		"motion",
		"form_factor",
		"video_companion",
		"video_session",
		"wearable",
		"device_mesh",
		"bluetooth_audio",
		"learning_visualization",
		"image_privacy",
	}
	for _, module := range modules {
		t.Run(module, func(t *testing.T) {
			path := filepath.Join(root, module, "service", "service.go")
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("service source missing: %v", err)
			}
			file, err := parser.ParseFile(
				token.NewFileSet(),
				path,
				nil,
				parser.ParseComments,
			)
			if err != nil {
				t.Fatalf("parse service source: %v", err)
			}
			for _, declaration := range file.Decls {
				function, ok := declaration.(*ast.FuncDecl)
				if !ok || function.Recv == nil || function.Name.Name != "CheckReadiness" {
					continue
				}
				if function.Type.Results == nil || len(function.Type.Results.List) != 2 {
					t.Fatalf("CheckReadiness must return readiness and error")
				}
				return
			}
			t.Fatal("service does not expose CheckReadiness")
		})
	}
}

func capabilityModulesRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test path")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "modules"))
	if !strings.HasSuffix(root, filepath.Join("internal", "modules")) {
		t.Fatalf("unexpected capability module root %q", root)
	}
	return root
}
