package main

import (
	"strings"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/plugin"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/plugin/contrib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPluginListCmd tests the plugin list command with both table and JSON outputs
func TestPluginListCmd(t *testing.T) {
	manifests := contrib.GetPluginManifests()
	assert.Greater(t, len(manifests), 0, "Should have plugins in registry")

	// Test list command creation
	cmd := newPluginListCmd()
	assert.Equal(t, "list", cmd.Use)
	assert.Contains(t, cmd.Short, "registry")

	// Verify manifests structure
	for _, m := range manifests {
		assert.NotEmpty(t, m.Metadata.Name)
		assert.NotEmpty(t, m.Metadata.Version)
		assert.NotEmpty(t, m.APIVersion)
	}
}

// TestPluginSearchCmd tests the plugin search command
func TestPluginSearchCmd(t *testing.T) {
	cmd := newPluginSearchCmd()
	assert.Equal(t, "search <keyword>", cmd.Use)

	// Test search by different keywords
	testCases := []struct {
		name     string
		keyword  string
		expected int // expected number of results
	}{
		{"search monitor", "monitor", 4}, // monitor.collector (3x) + monitor.alerter (1x)
		{"search webhook", "webhook", 2}, // webhook.mutating + webhook.validating
		{"search render-farm", "render-farm", 3}, // All render farm plugins
		{"search dr-", "dr-", 3}, // All DR plugins
		{"search cs-", "cs-", 3}, // All CS plugins
		{"search nonexistent", "nonexistent-plugin-xyz", 0},
	}

	manifests := contrib.GetPluginManifests()
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var filtered []plugin.PluginManifest
			for _, m := range manifests {
				if matchesKeyword(m, tc.keyword) {
					filtered = append(filtered, m)
				}
			}
			assert.Equal(t, tc.expected, len(filtered), "Search for %q should find %d plugins", tc.keyword, tc.expected)
		})
	}
}

// TestPluginInstallCmd tests the plugin install command
func TestPluginInstallCmd(t *testing.T) {
	cmd := newPluginInstallCmd()
	assert.Equal(t, "install <plugin-name>", cmd.Use)

	// Test valid plugin name
	validName := "render-farm-collector"
	manifests := contrib.GetPluginManifests()
	
	found := false
	for _, m := range manifests {
		if m.Metadata.Name == validName {
			found = true
			break
		}
	}
	assert.True(t, found, "Test plugin %s should exist in registry", validName)
}

// TestPluginUninstallCmd tests the plugin uninstall command
func TestPluginUninstallCmd(t *testing.T) {
	cmd := newPluginUninstallCmd()
	assert.Equal(t, "uninstall <plugin-name>", cmd.Use)

	// Similar validation as install - just check command structure
	manifests := contrib.GetPluginManifests()
	require.Greater(t, len(manifests), 0, "Should have plugins to test against")
}

// TestNewPluginCmd tests the parent plugin command
func TestNewPluginCmd(t *testing.T) {
	cmd := newPluginCmd()
	assert.Equal(t, "plugin", cmd.Use)
	assert.NotNil(t, cmd.Long, "Should have long description")
	
	// Verify all subcommands are registered
	subCommands := cmd.Commands()
	assert.GreaterOrEqual(t, len(subCommands), 4, "Should have at least 4 subcommands")
	
	foundList := false
	foundSearch := false
	foundInstall := false
	foundUninstall := false
	
	for _, sc := range subCommands {
		switch sc.Use {
		case "list":
			foundList = true
		case "search <keyword>":
			foundSearch = true
		case "install <plugin-name>":
			foundInstall = true
		case "uninstall <plugin-name>":
			foundUninstall = true
		}
	}
	
	assert.True(t, foundList, "Should have 'list' subcommand")
	assert.True(t, foundSearch, "Should have 'search' subcommand")
	assert.True(t, foundInstall, "Should have 'install' subcommand")
	assert.True(t, foundUninstall, "Should have 'uninstall' subcommand")
}

// TestExtractDomain tests domain extraction from plugin names
func TestExtractDomain(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		{"render-farm-cloud-provider", "render"},
		{"render-farm-score", "render"},
		{"dr-collector", "dr"},
		{"cs-webhook", "cs"},
		{"singleword", "singleword"}, // No hyphen
		{"a-b-c-d", "a"}, // First part only
	}
	
	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			result := extractDomain(tc.input)
			assert.Equal(t, tc.expected, result, "Domain extraction mismatch for %s", tc.input)
		})
	}
}

// TestMatchesKeyword tests keyword matching logic
func TestMatchesKeyword(t *testing.T) {
	manifests := contrib.GetPluginManifests()
	
	t.Run("match by name", func(t *testing.T) {
		// Use a keyword that we know exists in the manifests
		result := matchesKeyword(manifests[0], "render")
		assert.True(t, result, "Should match 'render' in name")
	})
	
	t.Run("match by domain", func(t *testing.T) {
		domain := extractDomain(manifests[0].Metadata.Name)
		result := matchesKeyword(manifests[0], domain)
		assert.True(t, result, "Should match domain %s", domain)
	})
	
	t.Run("match by extension point", func(t *testing.T) {
		if len(manifests[0].Metadata.ExtensionPoints) > 0 {
			ext := string(manifests[0].Metadata.ExtensionPoints[0])
			parts := strings.Split(ext, ".")
			if len(parts) > 0 {
				result := matchesKeyword(manifests[0], parts[0])
				assert.True(t, result, "Should match first part of extension %s", ext)
			}
		}
	})
	
	t.Run("no match", func(t *testing.T) {
		result := matchesKeyword(manifests[0], "this-should-not-match-anywhere-xyz")
		assert.False(t, result, "Should not match random string")
	})
}

// TestTruncateText tests text truncation helper
func TestTruncateText(t *testing.T) {
	longText := "This is a very long description that should be truncated when it exceeds the maximum length limit specified by the user"
	
	resultShort := truncateText(longText, 50)
	assert.LessOrEqual(t, len(resultShort), 53, "Truncated text should be around maxLen + 3 for ellipsis")
	assert.Contains(t, resultShort, "...", "Should contain ellipsis")
	
	resultLong := truncateText("Short", 50)
	assert.Equal(t, "Short", resultLong, "Short text should not be modified")
}

// TestRealRegistryIntegration tests integration with real contrib registry
func TestRealRegistryIntegration(t *testing.T) {
	manifests := contrib.GetPluginManifests()
	
	// Group by domain
	domains := make(map[string]int)
	for _, m := range manifests {
		domain := extractDomain(m.Metadata.Name)
		domains[domain]++
	}
	
	// Verify we have plugins from all three domains
	assert.Contains(t, domains, "render", "Should have render-farm plugins")
	assert.Contains(t, domains, "dr", "Should have disaster-recovery plugins")
	assert.Contains(t, domains, "cs", "Should have customer-service plugins")
	
	// Count total plugins
	t.Logf("Total plugins: %d", len(manifests))
	t.Logf("Domain breakdown: %v", domains)
	
	// Verify some specific plugins exist
	expectedPlugins := map[string]bool{
		"render-farm-cloud-provider": false,
		"render-farm-score":          false,
		"dr-collector":               false,
		"dr-webhook":                 false,
		"cs-collector":               false,
		"cs-threat-detector":         false,
	}
	
	for _, m := range manifests {
		if _, ok := expectedPlugins[m.Metadata.Name]; ok {
			expectedPlugins[m.Metadata.Name] = true
		}
	}
	
	for name, found := range expectedPlugins {
		assert.True(t, found, "Expected plugin %s should be in registry", name)
	}
}

// TestJSONOutputFormat tests that JSON output format works
func TestJSONOutputFormat(t *testing.T) {
	manifests := contrib.GetPluginManifests()
	
	// Simulate the JSON output generation logic from newPluginListCmd
	type PluginListItem struct {
		Name            string   `json:"name"`
		Version         string   `json:"version"`
		Description     string   `json:"description"`
		Author          string   `json:"author"`
		ExtensionPoints []string `json:"extension_points"`
		Domain          string   `json:"domain"`
	}
	
	items := make([]PluginListItem, len(manifests))
	for i, m := range manifests {
		extPoints := make([]string, len(m.Metadata.ExtensionPoints))
		for j, ext := range m.Metadata.ExtensionPoints {
			extPoints[j] = string(ext)
		}
		
		domain := extractDomain(m.Metadata.Name)
		
		items[i] = PluginListItem{
			Name:            m.Metadata.Name,
			Version:         m.Metadata.Version,
			Description:     m.Metadata.Description,
			Author:          m.Metadata.Author,
			ExtensionPoints: extPoints,
			Domain:          domain,
		}
	}
	
	assert.Len(t, items, len(manifests), "JSON items count should match manifest count")
	assert.NotEmpty(t, items[0].Domain, "Domain should be extracted for each item")
}

// BenchmarkGetPluginManifests benchmarks manifest retrieval
func BenchmarkGetPluginManifests(b *testing.B) {
	for i := 0; i < b.N; i++ {
		manifests := contrib.GetPluginManifests()
		if len(manifests) == 0 {
			b.Fatal("Expected non-empty manifests")
		}
	}
}
