package api

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// VersionManager manages API versioning and deprecation
type VersionManager struct {
	mu              sync.RWMutex
	versions        map[string]*APIVersion
	currentVersion  string
	defaultVersions []string
	deprecatedUntil time.Time
}

// APIVersion defines API version configuration
type APIVersion struct {
	Version      string                   `json:"version"`
	Status       VersionStatus            `json:"status"` // stable, beta, deprecated, sunset
	ReleaseDate  time.Time                `json:"release_date"`
	Deprecation  *DeprecationInfo         `json:"deprecation,omitempty"`
	SunsetDate   *time.Time               `json:"sunset_date,omitempty"`
	Changelog    []ChangelogEntry         `json:"changelog,omitempty"`
	MigrationGuide *MigrationGuide        `json:"migration_guide,omitempty"`
	Features     []FeatureList           `json:"features,omitempty"`
	Routes       map[string]*RouteConfig `json:"routes,omitempty"`
	Patches      []*VersionPatch         `json:"patches,omitempty"`
}

// VersionStatus defines API version lifecycle
type VersionStatus string

const (
	VersionStable   VersionStatus = "stable"
	VersionBeta     VersionStatus = "beta"
	VersionDeprecated VersionStatus = "deprecated"
	VersionSunset   VersionStatus = "sunset"
)

// DeprecationInfo contains deprecation details
type DeprecationInfo struct {
	Message          string                    `json:"message"`
	SupportEnds      time.Time                 `json:"support_ends"`
	DocumentationURL string                    `json:"documentation_url"`
	Alternative      string                    `json:"alternative,omitempty"`
}

// ChangelogEntry records change in version
type ChangelogEntry struct {
	Date       time.Time `json:"date"`
	Type       string    `json:"type"`          // added, changed, deprecated, removed, fixed
	Description string   `json:"description"`
	Credits    []string  `json:"credits,omitempty"`
	Breaking   bool      `json:"breaking"`
}

// MigrationGuide provides guidance for migrating between versions
type MigrationGuide struct {
	FromVersion string           `json:"from_version"`
	ToVersion   string           `json:"to_version"`
	Steps       []MigrationStep  `json:"steps"`
	Examples    []MigrationExample `json:"examples,omitempty"`
	Timeline    string           `json:"timeline"`
}

// MigrationStep describes single migration step
type MigrationStep struct {
	Order     int    `json:"order"`
	Title     string `json:"title"`
	Description string `json:"description"`
	CodeDiff  string `json:"code_diff,omitempty"`
}

// MigrationExample shows before/after code
type MigrationExample struct {
	Languages []LanguageExample `json:"languages"`
}

// LanguageExample shows example in specific language
type LanguageExample struct {
	Language string `json:"language"`
	Before   string `json:"before"`
	After    string `json:"after"`
}

// RouteConfig defines route-level version config
type RouteConfig struct {
	Method   string   `json:"method"`
	Path     string   `json:"path"`
	Versions []string `json:"versions"` // Supported versions
	Redirect string   `json:"redirect,omitempty"` // Redirect to new path
}

// VersionPatch documents breaking changes requiring patch
type VersionPatch struct {
	Name        string        `json:"name"`
	Version     string        `json:"version"`
	Description string        `json:"description"`
	Diff        PatchDiff     `json:"diff"`
	Severity    SeverityLevel `json:"severity"`
	FixUrl      string        `json:"fix_url,omitempty"`
}

// PatchDiff shows what changed
type PatchDiff struct {
	Request  map[string]interface{} `json:"request"`
	Response map[string]interface{} `json:"response"`
	Error    map[string]interface{} `json:"error,omitempty"`
}

// SeverityLevel defines patch severity
type SeverityLevel string

const (
	SeverityLow    SeverityLevel = "low"
	SeverityMedium SeverityLevel = "medium"
	SeverityHigh   SeverityLevel = "high"
	SeverityCritical SeverityLevel = "critical"
)

// FeatureList describes API feature availability
type FeatureList struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Status      string   `json:"status"` // available, beta, deprecated
	AvailableIn []string `json:"available_in"`
}

// NewVersionManager creates API version manager
func NewVersionManager() *VersionManager {
	vm := &VersionManager{
		versions: make(map[string]*APIVersion),
		mu: sync.RWMutex{},
	}
	
	// Register default versions
	vm.RegisterVersion(&APIVersion{
		Version:     "v1",
		Status:      VersionStable,
		ReleaseDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	
	vm.RegisterVersion(&APIVersion{
		Version:     "v2",
		Status:      VersionBeta,
		ReleaseDate: time.Now(),
		Features: []FeatureList{
			{Name: "OAuth2.1 Support", Status: "beta"},
			{Name: "GraphQL Hybrid Mode", Status: "available"},
		},
	})
	
	return vm
}

// RegisterVersion adds new API version
func (vm *VersionManager) RegisterVersion(version *APIVersion) {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	
	if version.Version == "" {
		version.Version = fmt.Sprintf("v%d-%03d", time.Now().Unix(), time.Now().Unix()%1000)
	}
	
	vm.versions[version.Version] = version
	vm.defaultVersions = append(vm.defaultVersions, version.Version)
}

// GetVersion retrieves API version by ID
func (vm *VersionManager) GetVersion(versionID string) (*APIVersion, bool) {
	vm.mu.RLock()
	defer vm.mu.RUnlock()
	
	version, exists := vm.versions[versionID]
	return version, exists
}

// GetCurrentVersion returns the latest stable version
func (vm *VersionManager) GetCurrentVersion() string {
	vm.mu.RLock()
	defer vm.mu.RUnlock()
	
	for _, v := range vm.defaultVersions {
		if version, exists := vm.versions[v]; exists && version.Status == VersionStable {
			return v
		}
	}
	
	return "v1"
}

// MarkVersionDeprecated marks version as deprecated with notice
func (vm *VersionManager) MarkVersionDeprecated(versionID, message string, supportEnd time.Duration) {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	
	version, exists := vm.versions[versionID]
	if !exists {
		return
	}
	
	version.Status = VersionDeprecated
	version.Deprecation = &DeprecationInfo{
		Message:     message,
		SupportEnds: time.Now().Add(supportEnd),
	}
}

// AddChangelogEntry adds changelog entry to version
func (vm *VersionManager) AddChangelogEntry(versionID string, entry ChangelogEntry) {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	
	version, exists := vm.versions[versionID]
	if !exists {
		return
	}
	
	version.Changelog = append(version.Changelog, entry)
}

// CreateMigrationGuide generates migration guide between versions
func (vm *VersionManager) CreateMigrationGuide(from, to string) (*MigrationGuide, error) {
	vm.mu.RLock()
	defer vm.mu.RUnlock()
	
	fromVer, fromExists := vm.versions[from]
	toVer, toExists := vm.versions[to]
	
	if !fromExists || !toExists {
		return nil, fmt.Errorf("invalid versions")
	}
	
	guide := &MigrationGuide{
		FromVersion: from,
		ToVersion:   to,
		Timeline:    "recommended within 6 months",
	}
	
	// Analyze differences between versions
	diffSteps := vm.analyzeVersionDifferences(fromVer, toVer)
	guide.Steps = diffSteps
	
	// Add examples for common migrations
	guide.Examples = vm.generateMigrationExamples(fromVer, toVer)
	
	return guide, nil
}

func (vm *VersionManager) analyzeVersionDifferences(from, to *APIVersion) []MigrationStep {
	var steps []MigrationStep
	
	if len(to.Changelog) > 0 {
		steps = append(steps, MigrationStep{
			Order: 1,
			Title: "Review Breaking Changes",
			Description: "The following breaking changes will affect your integration:",
		})
		
		for i, entry := range to.Changelog {
			if entry.Breaking {
				steps[i].Description += fmt.Sprintf("\n- [%s]: %s", entry.Type, entry.Description)
			}
		}
	}
	
	steps = append(steps, MigrationStep{
		Order:     len(steps) + 1,
		Title:     "Update Authentication",
		Description: "Implement new OAuth flows if required by target version",
	})
	
	steps = append(steps, MigrationStep{
		Order:     len(steps) + 1,
		Title:     "Test in Sandbox",
		Description: "Test all changes in staging environment before production deployment",
	})
	
	return steps
}

func (vm *VersionManager) generateMigrationExamples(from, to *APIVersion) []MigrationExample {
	examples := []MigrationExample{}
	
	// Add generic authentication example
	examples = append(examples, MigrationExample{
		Languages: []LanguageExample{
			{
				Language: "curl",
				Before:   "curl -H \"Authorization: Bearer OLD_TOKEN\"",
				After:    "curl -H \"Authorization: Bearer NEW_TOKEN_v2\"",
			},
			{
				Language: "Go",
				Before:   `client := NewClient("OLD_API_KEY")`,
				After:    `client := NewClient("NEW_API_KEY", ClientWithOAuth2())`,
			},
		},
	})
	
	return examples
}

// CheckAcceptHeader determines which API version the client wants
func CheckAcceptHeader(acceptHeader string) string {
	if acceptHeader == "" {
		return "" // No preference specified
	}
	
	parts := strings.Split(acceptHeader, ",")
	for _, part := range parts {
		pair := strings.SplitN(part, ";", 2)
		contentType := strings.TrimSpace(pair[0])
		
		if strings.Contains(contentType, "application/vnd.cloudai-fusion.v") {
			switch contentType {
			case "application/vnd.cloudai-fusion.v1":
				return "v1"
			case "application/vnd.cloudai-fusion.v2":
				return "v2"
			}
		}
	}
	
	return ""
}

// Middleware handlers
type VersionMiddleware struct {
	manager *VersionManager
}

func NewVersionMiddleware(manager *VersionManager) *VersionMiddleware {
	return &VersionMiddleware{manager: manager}
}

// HandleVersionNegotiation handles content negotiation based on Accept header
func (mw *VersionMiddleware) HandleVersionNegotiation(c *gin.Context) {
	acceptHeader := c.GetHeader("Accept")
	
	if acceptHeader != "" {
		requestedVersion := CheckAcceptHeader(acceptHeader)
		if requestedVersion != "" {
			c.Set("requested_api_version", requestedVersion)
			
			version, exists := mw.manager.GetVersion(requestedVersion)
			if !exists {
				c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
					"error": "Unsupported API version requested",
				})
				return
			}
			
			if version.Status == VersionDeprecated {
				c.Header("X-API-Deprecation", "true")
				c.Header("Warning", "299 - This API version is deprecated")
			}
			
			c.Next()
			return
		}
	}
	
	// Default to current stable version
	current := mw.manager.GetCurrentVersion()
	c.Set("api_version", current)
	c.Set("default_api_version", true)
	
	c.Next()
}

// HandleDeprecationNotice adds deprecation headers to response
func (mw *VersionMiddleware) HandleDeprecationNotice(c *gin.Context) {
	version := c.GetString("api_version")
	
	apiVersion, exists := mw.manager.GetVersion(version)
	if exists && apiVersion.Status == VersionDeprecated {
		if apiVersion.Deprecation != nil {
			c.Header("X-API-Deprecated", "true")
			c.Header("X-API-Support-Ends", apiVersion.Deprecation.SupportEnds.Format(time.RFC3339))
			c.Header("Link", "<"+apiVersion.Deprecation.DocumentationURL+">; rel=\"describedby\"")
		}
	}
	
	c.Next()
}

// RegisterRoutes registers version management routes
func (mw *VersionMiddleware) RegisterRoutes(r *gin.Engine) {
	// Version info endpoints
	r.GET("/api/version", mw.handleVersionInfo)
	r.GET("/api/versions", mw.handleVersionList)
	r.GET("/api/versions/:version/migrate-to/:target", mw.handleMigrationGuide)
	
	// Version history
	r.GET("/api/changelog/:version", mw.handleChangelog)
	r.GET("/api/versions/:version/patches", mw.handleVersionPatches)
}

// API Handlers
func (mw *VersionMiddleware) handleVersionInfo(c *gin.Context) {
	current := mw.manager.GetCurrentVersion()
	version, _ := mw.manager.GetVersion(current)
	
	c.JSON(http.StatusOK, gin.H{
		"current_version": current,
		"version_info":    version,
	})
}

func (mw *VersionMiddleware) handleVersionList(c *gin.Context) {
	mw.manager.mu.RLock()
	defer mw.manager.mu.RUnlock()
	
	result := make(map[string]APIVersion)
	for k, v := range mw.manager.versions {
		result[k] = *v
	}
	
	c.JSON(http.StatusOK, result)
}

func (mw *VersionMiddleware) handleMigrationGuide(c *gin.Context) {
	from := c.Param("version")
	to := c.Param("target")
	
	guide, err := mw.manager.CreateMigrationGuide(from, to)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	
	c.JSON(http.StatusOK, gin.H{
		"migration_guide": guide,
	})
}

func (mw *VersionMiddleware) handleChangelog(c *gin.Context) {
	version := c.Param("version")
	
	apiVersion, exists := mw.manager.GetVersion(version)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "version not found"})
		return
	}
	
	c.JSON(http.StatusOK, apiVersion.Changelog)
}

func (mw *VersionMiddleware) handleVersionPatches(c *gin.Context) {
	version := c.Param("version")
	
	apiVersion, exists := mw.manager.GetVersion(version)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "version not found"})
		return
	}
	
	c.JSON(http.StatusOK, apiVersion.Patches)
}
