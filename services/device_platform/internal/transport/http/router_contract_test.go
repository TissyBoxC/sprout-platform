package http

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var registeredRoutePattern = regexp.MustCompile(
	`"(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)[ \t]+([^"\s]+)"`,
)

func TestEveryRegisteredRouteIsDocumentedAndNoExtraRoutesRemain(t *testing.T) {
	registered := registeredHTTPRoutes(t)
	documented := documentedOpenAPIRoutes(t)

	var missing []string
	for route := range registered {
		if !documented[route] {
			missing = append(missing, route)
		}
	}
	var extra []string
	for route := range documented {
		if !registered[route] {
			extra = append(extra, route)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)

	if len(missing) > 0 {
		t.Fatalf("registered routes missing from OpenAPI: %s", strings.Join(missing, ", "))
	}
	if len(extra) > 0 {
		t.Fatalf("OpenAPI routes with no registered handler: %s", strings.Join(extra, ", "))
	}
}

func registeredHTTPRoutes(t *testing.T) map[string]bool {
	t.Helper()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read transport package: %v", err)
	}

	routes := make(map[string]bool)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") ||
			strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		body, err := os.ReadFile(entry.Name())
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		for _, match := range registeredRoutePattern.FindAllStringSubmatch(string(body), -1) {
			routes[match[1]+" "+match[2]] = true
		}
	}
	if len(routes) == 0 {
		t.Fatal("no registered HTTP routes found")
	}
	return routes
}

func documentedOpenAPIRoutes(t *testing.T) map[string]bool {
	t.Helper()

	body, err := os.ReadFile(filepath.Join("..", "..", "contracts", "http", "openapi.yaml"))
	if err != nil {
		t.Fatalf("read OpenAPI contract: %v", err)
	}

	routes := make(map[string]bool)
	inPaths := false
	currentPath := ""
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSuffix(line, "\r")
		switch {
		case line == "paths:":
			inPaths = true
			currentPath = ""
		case !inPaths:
			continue
		case strings.HasPrefix(line, "components:"):
			inPaths = false
		case strings.HasPrefix(line, "  /"):
			currentPath = strings.TrimSuffix(strings.TrimSpace(line), ":")
		case currentPath == "":
			continue
		case strings.HasPrefix(line, "    ") && !strings.HasPrefix(line, "      "):
			method := strings.TrimSuffix(strings.TrimSpace(line), ":")
			switch method {
			case "get", "post", "put", "patch", "delete", "head", "options":
				routes[strings.ToUpper(method)+" "+currentPath] = true
			}
		}
	}
	if len(routes) == 0 {
		t.Fatal("no HTTP operations found in OpenAPI contract")
	}
	return routes
}
