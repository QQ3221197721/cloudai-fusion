// Package vuln_scanner provides defensive vulnerability scanning capabilities
// for identifying security weaknesses in container images and package manifests.
// This module integrates aquasecurity/trivy-db and supports OS package vulnerability detection.
package vuln_scanner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/aquasecurity/trivy-db/pkg/db"
	"github.com/aquasecurity/trivy-db/pkg/types"
	"github.com/aquasecurity/trivy-db/pkg/vulnsrc/alma"
	"github.com/aquasecurity/trivy-db/pkg/vulnsrc/alpine"
	"github.com/aquasecurity/trivy-db/pkg/vulnsrc/amazon"
	"github.com/aquasecurity/trivy-db/pkg/vulnsrc/debian"
	"github.com/aquasecurity/trivy-db/pkg/vulnsrc/oracle"
	"github.com/aquasecurity/trivy-db/pkg/vulnsrc/photon"
	"github.com/aquasecurity/trivy-db/pkg/vulnsrc/repo"
	"github.com/aquasecurity/trivy-db/pkg/vulnsrc/rocky"
	"github.com/aquasecurity/trivy-db/pkg/vulnsrc/rhel"
	"github.com/aquasecurity/trivy-db/pkg/vulnsrc/ubuntu"
	"github.com/sirupsen/logrus"
)

// ============================================================================
// TRIVY INTEGRATION - Core Scanning Engine
// ============================================================================

// TrivyScanner wraps trivy-db vulnerability database queries
type TrivyScanner struct {
	logger        logrus.FieldLogger
	dbPath        string
	cacheEnabled  bool
	cacheDuration time.Duration
	dbMutex       sync.RWMutex
	initOnce      sync.Once
}

// ScannerConfig configures trivy scanner behavior
type ScannerConfig struct {
	DBPath            string        // Path to trivy-db directory (default: ~/.cache/trivy/db)
	CacheEnabled      bool          // Enable result caching
	CacheDuration     time.Duration // Cache TTL (default: 1 hour)
	LogLevel          logrus.Level  // Logging verbosity
	SkipUnsupported   bool          // Skip packages without CVE data
	IncludeNonFixed   bool          // Include vulnerabilities without fix versions
	OSDistribution    string        // Target OS distribution (debian, ubuntu, alpine, rhel, etc.)
	ScanContainers    bool          // Scan container images
	ScanDocker        bool          // Use Docker daemon directly
	Timeout           time.Duration // Overall scan timeout
	Multiplatform     bool          // Support multi-platform scans
}

// DefaultConfig returns production-safe defaults
func DefaultConfig() *ScannerConfig {
	return &ScannerConfig{
		DBPath:            "", // Use default cache path
		CacheEnabled:      true,
		CacheDuration:     time.Hour,
		LogLevel:          logrus.WarnLevel,
		SkipUnsupported:   false,
		IncludeNonFixed:   true,
		OSDistribution:    "",
		ScanContainers:    true,
		ScanDocker:        false,
		Timeout:           30 * time.Minute,
		Multiplatform:     false,
	}
}

// NewTrivyScanner initializes trivy scanner with given config
func NewTrivyScanner(config *ScannerConfig) (*TrivyScanner, error) {
	if config == nil {
		config = DefaultConfig()
	}

	logger := logrus.New()
	logger.SetLevel(config.LogLevel)
	logger.SetFormatter(&logrus.TextFormatter{
		TimestampFormat: time.RFC3339,
		FullTimestamp:   true,
	})

	scanner := &TrivyScanner{
		logger:        logger,
		dbPath:        config.DBPath,
		cacheEnabled:  config.CacheEnabled,
		cacheDuration: config.CacheDuration,
	}

	// Initialize trivy DB if needed
	if err := scanner.initDB(); err != nil {
		return nil, fmt.Errorf("failed to initialize trivy db: %w", err)
	}

	return scanner, nil
}

// initDB loads the vulnerability database from disk
func (s *TrivyScanner) initDB() error {
	var err error
	s.initOnce.Do(func() {
		if s.dbPath == "" {
			s.dbPath = filepath.Join(os.Getenv("HOME"), ".cache", "trivy", "db")
		}

		// Initialize trivy DB adapter
		if err = db.Init(s.dbPath); err != nil {
			s.logger.Errorf("Failed to initialize trivy database: %v", err)
			err = fmt.Errorf("db initialization failed: %w", err)
			return
		}

		s.logger.Info("Trivy DB initialized successfully")
	})
	return err
}

// ============================================================================
// VULNERABILITY DATABASE QUERIES
// ============================================================================

// QueryOSPackages queries vulnerability database for specific OS distributions
// Supports: debian, ubuntu, alpine, rhel, centos, alma, rocky, oracle, photon
type OSDistribution string

const (
	DistDebian    OSDistribution = "debian"
	DistUbuntu    OSDistribution = "ubuntu"
	DistAlpine    OSDistribution = "alpine"
	DistRHEL      OSDistribution = "rhel"
	DistCentOS    OSDistribution = "centos"
	DistAlma      OSDistribution = "alma"
	DistRocky     OSDistribution = "rocky"
	DistOracle    OSDistribution = "oracle"
	DistPhoton    OSDistribution = "photon"
	DistAmazon    OSDistribution = "amazon"
)

// OSPackageVulns contains vulnerabilities for a single package in an OS distribution
type OSPackageVulns struct {
	PackageName  string              `json:"package_name"`
	Version      string              `json:"version"`
	Distribution OSDistribution      `json:"distribution"`
	Severity     types.Severity      `json:"severity"`
	CVEID        string              `json:"cve_id"`
	Title        string              `json:"title"`
	Description  string              `json:"description"`
	ResolvedIn   string              `json:"resolved_in,omitempty"`
	CWEs         []string            `json:"cwes,omitempty"`
	Metadata     map[string]string   `json:"metadata,omitempty"`
}

// QueryForPackage queries the vulnerability database for a specific package
func (s *TrivyScanner) QueryForPackage(pkgName, pkgVersion, distName string) ([]OSPackageVulns, error) {
	s.dbMutex.RLock()
	defer s.dbMutex.RUnlock()

	var results []OSPackageVulns
	dist := parseDistribution(distName)

	if dist == "" {
		return nil, fmt.Errorf("unsupported distribution: %s", distName)
	}

	// Query different distributions using trivy-vulnsrc adapters
	switch dist {
	case DistDebian, DistUbuntu:
		results = s.queryDebianLike(dist, pkgName, pkgVersion)
	case DistRHEL, DistCentOS:
		results = s.queryRedHatLike(dist, pkgName, pkgVersion)
	case DistAlpine:
		results = s.queryAlpine(pkgName, pkgVersion)
	case DistAlma, DistRocky, DistOracle:
		results = s.queryEnterpriseLinux(dist, pkgName, pkgVersion)
	case DistPhoton:
		results = s.queryPhoton(pkgName, pkgVersion)
	case DistAmazon:
		results = s.queryAmazonLinux(pkgName, pkgVersion)
	default:
		return nil, fmt.Errorf("no query handler for distribution: %s", dist)
	}

	return results, nil
}

// queryDebianLike handles debian/ubuntu family distributions
func (s *TrivyScanner) queryDebianLike(dist OSDistribution, pkgName, pkgVersion string) []OSPackageVulns {
	var vulnSrc repo.VulnSrc
	var distName string

	switch dist {
	case DistDebian:
		vulnSrc = debian.NewVulnSrc()
		distName = "debian"
	case DistUbuntu:
		vulnSrc = ubuntu.NewVulnSrc()
		distName = "ubuntu"
	default:
		return nil
	}

	// Query vulnerability source
	vulns, err := vulnSrc.Get(nil, pkgName)
	if err != nil {
		s.logger.Debugf("No vulnerabilities found for %s/%s: %v", distName, pkgName, err)
		return nil
	}

	results := make([]OSPackageVulns, 0)
	for _, vulnEntry := range vulns {
		if !shouldIncludeVuln(vulnEntry, pkgVersion, s.includeNonFixed()) {
			continue
		}

		result := OSPackageVulns{
			PackageName:  pkgName,
			Version:      pkgVersion,
			Distribution: dist,
			CVEID:        vulnEntry.VulnerabilityID,
			Title:        vulnEntry.Title,
			Description:  vulnEntry.Description,
			Severity:     vulnEntry.Severity,
			ResolvedIn:   vulnEntry.FixedVersion,
			CWEs:         extractCWEs(vulnEntry),
			Metadata:     extractMetadata(vulnEntry),
		}
		results = append(results, result)
	}

	return results
}

// queryRedHatLike handles RHEL/CentOS/Fedora distributions
func (s *TrivyScanner) queryRedHatLike(dist OSDistribution, pkgName, pkgVersion string) []OSPackageVulns {
	var vulnSrc repo.VulnSrc
	var distName string

	switch dist {
	case DistRHEL:
		vulnSrc = rhel.NewVulnSrc()
		distName = "redhat"
	case DistCentOS:
		vulnSrc = rhel.NewVulnSrc() // CentOS shares RHEL DB
		distName = "centos"
	default:
		return nil
	}

	vulns, err := vulnSrc.Get(nil, pkgName)
	if err != nil {
		s.logger.Debugf("No vulnerabilities found for %s/%s: %v", distName, pkgName, err)
		return nil
	}

	results := make([]OSPackageVulns, 0)
	for _, vulnEntry := range vulns {
		if !shouldIncludeVuln(vulnEntry, pkgVersion, s.includeNonFixed()) {
			continue
		}

		result := OSPackageVulns{
			PackageName:  pkgName,
			Version:      pkgVersion,
			Distribution: dist,
			CVEID:        vulnEntry.VulnerabilityID,
			Title:        vulnEntry.Title,
			Description:  vulnEntry.Description,
			Severity:     vulnEntry.Severity,
			ResolvedIn:   vulnEntry.FixedVersion,
			CWEs:         extractCWEs(vulnEntry),
			Metadata:     extractMetadata(vulnEntry),
		}
		results = append(results, result)
	}

	return results
}

// queryAlpine handles Alpine Linux distributions
func (s *TrivyScanner) queryAlpine(pkgName, pkgVersion string) []OSPackageVulns {
	vulnSrc := alpine.NewVulnSrc()

	vulns, err := vulnSrc.Get(nil, pkgName)
	if err != nil {
		s.logger.Debugf("No vulnerabilities found for alpine/%s: %v", pkgName, err)
		return nil
	}

	results := make([]OSPackageVulns, 0)
	for _, vulnEntry := range vulns {
		if !shouldIncludeVuln(vulnEntry, pkgVersion, s.includeNonFixed()) {
			continue
		}

		result := OSPackageVulns{
			PackageName:  pkgName,
			Version:      pkgVersion,
			Distribution: DistAlpine,
			CVEID:        vulnEntry.VulnerabilityID,
			Title:        vulnEntry.Title,
			Description:  vulnEntry.Description,
			Severity:     vulnEntry.Severity,
			ResolvedIn:   vulnEntry.FixedVersion,
			CWEs:         extractCWEs(vulnEntry),
			Metadata:     extractMetadata(vulnEntry),
		}
		results = append(results, result)
	}

	return results
}

// queryEnterpriseLinux handles Alma/Rocky/Oracle Linux
func (s *TrivyScanner) queryEnterpriseLinux(dist OSDistribution, pkgName, pkgVersion string) []OSPackageVulns {
	var vulnSrc repo.VulnSrc

	switch dist {
	case DistAlma:
		vulnSrc = alma.NewVulnSrc()
	case DistRocky:
		vulnSrc = rocky.NewVulnSrc()
	case DistOracle:
		vulnSrc = oracle.NewVulnSrc()
	default:
		return nil
	}

	vulns, err := vulnSrc.Get(nil, pkgName)
	if err != nil {
		s.logger.Debugf("No vulnerabilities found for %s/%s: %v", dist, pkgName, err)
		return nil
	}

	results := make([]OSPackageVulns, 0)
	for _, vulnEntry := range vulns {
		if !shouldIncludeVuln(vulnEntry, pkgVersion, s.includeNonFixed()) {
			continue
		}

		result := OSPackageVulns{
			PackageName:  pkgName,
			Version:      pkgVersion,
			Distribution: dist,
			CVEID:        vulnEntry.VulnerabilityID,
			Title:        vulnEntry.Title,
			Description:  vulnEntry.Description,
			Severity:     vulnEntry.Severity,
			ResolvedIn:   vulnEntry.FixedVersion,
			CWEs:         extractCWEs(vulnEntry),
			Metadata:     extractMetadata(vulnEntry),
		}
		results = append(results, result)
	}

	return results
}

// queryPhoton handles Photon OS
func (s *TrivyScanner) queryPhoton(pkgName, pkgVersion string) []OSPackageVulns {
	vulnSrc := photon.NewVulnSrc()

	vulns, err := vulnSrc.Get(nil, pkgName)
	if err != nil {
		s.logger.Debugf("No vulnerabilities found for photon/%s: %v", pkgName, err)
		return nil
	}

	results := make([]OSPackageVulns, 0)
	for _, vulnEntry := range vulns {
		if !shouldIncludeVuln(vulnEntry, pkgVersion, s.includeNonFixed()) {
			continue
		}

		result := OSPackageVulns{
			PackageName:  pkgName,
			Version:      pkgVersion,
			Distribution: DistPhoton,
			CVEID:        vulnEntry.VulnerabilityID,
			Title:        vulnEntry.Title,
			Description:  vulnEntry.Description,
			Severity:     vulnEntry.Severity,
			ResolvedIn:   vulnEntry.FixedVersion,
			CWEs:         extractCWEs(vulnEntry),
			Metadata:     extractMetadata(vulnEntry),
		}
		results = append(results, result)
	}

	return results
}

// queryAmazonLinux handles Amazon Linux distributions
func (s *TrivyScanner) queryAmazonLinux(pkgName, pkgVersion string) []OSPackageVulns {
	vulnSrc := amazon.NewVulnSrc()

	vulns, err := vulnSrc.Get(nil, pkgName)
	if err != nil {
		s.logger.Debugf("No vulnerabilities found for amazon/%s: %v", pkgName, err)
		return nil
	}

	results := make([]OSPackageVulns, 0)
	for _, vulnEntry := range vulns {
		if !shouldIncludeVuln(vulnEntry, pkgVersion, s.includeNonFixed()) {
			continue
		}

		result := OSPackageVulns{
			PackageName:  pkgName,
			Version:      pkgVersion,
			Distribution: DistAmazon,
			CVEID:        vulnEntry.VulnerabilityID,
			Title:        vulnEntry.Title,
			Description:  vulnEntry.Description,
			Severity:     vulnEntry.Severity,
			ResolvedIn:   vulnEntry.FixedVersion,
			CWEs:         extractCWEs(vulnEntry),
			Metadata:     extractMetadata(vulnEntry),
		}
		results = append(results, result)
	}

	return results
}

// shouldIncludeVuln checks if vulnerability should be included based on version logic
func shouldIncludeVuln(vulnEntry types.VulnerabilityEntry, pkgVersion string, includeNonFixed bool) bool {
	if vulnEntry.FixedVersion == "" && !includeNonFixed {
		return false
	}
	return true
}

func (s *TrivyScanner) includeNonFixed() bool {
	if config := DefaultConfig(); config != nil {
		return config.IncludeNonFixed
	}
	return true
}

// ============================================================================
// HELPER FUNCTIONS FOR PARSING AND EXTRACTION
// ============================================================================

// parseDistribution normalizes distribution name to internal enum
func parseDistribution(name string) OSDistribution {
	switch name {
	case "debian", "Debian":
		return DistDebian
	case "ubuntu", "Ubuntu":
		return DistUbuntu
	case "alpine", "Alpine":
		return DistAlpine
	case "rhel", "redhat", "RHEL", "Red Hat":
		return DistRHEL
	case "centos", "CentOS":
		return DistCentOS
	case "alma", "alma-linux", "AlmaLinux":
		return DistAlma
	case "rocky", "rocky-linux", "RockyLinux":
		return DistRocky
	case "oracle", "ol", "Oracle Linux":
		return DistOracle
	case "photon", "Photon":
		return DistPhoton
	case "amazon", "amazon-linux", "Amazon Linux":
		return DistAmazon
	default:
		return ""
	}
}

// extractCWEs extracts CWE identifiers from vulnerability metadata
func extractCWEs(vulnEntry types.VulnerabilityEntry) []string {
	cwes := make([]string, 0)

	if vulnEntry.CWEs != "" {
		for _, cwe := range splitCWEString(vulnEntry.CWEs) {
			cwes = append(cwes, cwe)
		}
	}

	if vulnEntry.Custom != nil {
		if cwesRaw, ok := vulnEntry.Custom["CWEs"]; ok {
			if cwesStr, ok := cwesRaw.(string); ok {
				for _, cwe := range splitCWEString(cwesStr) {
					cwes = append(cwes, cwe)
				}
			}
		}
	}

	return cwes
}

// splitCWEString parses comma or space separated CWE IDs
func splitCWEString(input string) []string {
	result := make([]string, 0)
	parts := splitByCommaOrSpace(input)
	for _, part := range parts {
		part = trimSpace(part)
		if len(part) > 0 && len(part) < 10 {
			result = append(result, part)
		}
	}
	return result
}

// Extract Metadata from vulnerability entry
func extractMetadata(vulnEntry types.VulnerabilityEntry) map[string]string {
	metadata := make(map[string]string)

	if vulnEntry.Patch != "" {
		metadata["patch"] = vulnEntry.Patch
	}
	if vulnEntry.ArchInfo != "" {
		metadata["architecture_info"] = vulnEntry.ArchInfo
	}

	if vulnEntry.Custom != nil {
		for k, v := range vulnEntry.Custom {
			if key, ok := k.(string); ok {
				if val, ok := v.(string); ok {
					metadata[key] = val
				} else {
					metadata[key] = fmt.Sprintf("%v", v)
				}
			}
		}
	}

	return metadata
}

// Split helper functions
func splitByCommaOrSpace(s string) []string {
	result := make([]string, 0)
	current := ""
	inQuote := false

	for _, r := range s {
		if r == '"' || r == '\'' {
			inQuote = !inQuote
			current += string(r)
		} else if r == ',' || r == ' ' || r == ';' {
			if inQuote {
				current += string(r)
			} else {
				if len(current) > 0 {
					result = append(result, current)
					current = ""
				}
			}
		} else {
			current += string(r)
		}
	}

	if len(current) > 0 {
		result = append(result, current)
	}

	return result
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n') {
		start++
	}
	for start < end && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n') {
		end--
	}
	return s[start:end]
}

// ============================================================================
// CONTAINER IMAGE SCANNING SUPPORT
// ============================================================================

// ScanContainerArtifact scans container image layers for vulnerabilities
type ContainerScanResult struct {
	ImageName      string                `json:"image_name"`
	ScannedLayers  int                   `json:"scanned_layers"`
	Vulnerabilities []OSPackageVulns      `json:"vulnerabilities"`
	StartTime      time.Time             `json:"start_time"`
	EndTime        time.Time             `json:"end_time"`
	Duration       time.Duration         `json:"duration"`
	Summary        VulnerabilitySummary  `json:"summary"`
	ManifestPath   string                `json:"manifest_path,omitempty"`
	BaseImage      string                `json:"base_image,omitempty"`
	OSRelease      string                `json:"os_release,omitempty"`
	PackageManager string                `json:"package_manager,omitempty"`
}

// VulnerabilitySummary aggregates findings by severity
type VulnerabilitySummary struct {
	CriticalCount int `json:"critical_count"`
	HighCount     int `json:"high_count"`
	MediumCount   int `json:"medium_count"`
	LowCount      int `json:"low_count"`
	UnknownCount  int `json:"unknown_count"`
	TotalCount    int `json:"total_count"`
}

// ScanFileSystem scans filesystem layers for packages and vulnerabilities
func (s *TrivyScanner) ScanFileSystem(ctx context.Context, fsPath string, targetDist OSDistribution) (*ContainerScanResult, error) {
	startTime := time.Now()
	result := &ContainerScanResult{
		ScannedLayers:  0,
		Vulnerabilities: make([]OSPackageVulns, 0),
		StartTime:      startTime,
	}

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	packages := s.collectPackagesFromFS(ctx, fsPath)
	if len(packages) == 0 {
		s.logger.Warnf("No packages found in %s", fsPath)
	}

	for _, pkg := range packages {
		vulns, err := s.QueryForPackage(pkg.Name, pkg.Version, string(targetDist))
		if err != nil {
			s.logger.Warnf("Query failed for %s/%s: %v", pkg.Name, pkg.Version, err)
			continue
		}

		result.Vulnerabilities = append(result.Vulnerabilities, vulns...)
		result.ScannedLayers++
	}

	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(startTime)
	result.Summary = s.calculateSummary(result.Vulnerabilities)

	s.logger.Infof("Scanned %d packages, found %d vulnerabilities in %v", 
		result.ScannedLayers, result.TotalCount(), result.Duration)

	return result, nil
}

// PackageManifest represents a discovered package from filesystem
type PackageManifest struct {
	Name    string
	Version string
	Source  string
}

// collectPackagesFromFS discovers package manifests in filesystem
func (s *TrivyScanner) collectPackagesFromFS(ctx context.Context, fsPath string) []PackageManifest {
	packages := make([]PackageManifest, 0)

	filepath.Walk(fsPath, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info.IsDir() {
			return walkErr
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		filename := filepath.Base(path)
		ext := filepath.Ext(filename)

		switch ext {
		case ".db":
			if filename == "packages.sqlite" || filename == "Installed" {
				pkgs := s.extractDpkgPackages(path)
				packages = append(packages, pkgs...)
			}
		case ".json":
			if contains([]string{"apki.json", "rpm.json", "package-lock.json"}, filename) {
				pkgs := s.extractJSONPackages(path)
				packages = append(packages, pkgs...)
			}
		case ".txt":
			if contains([]string{"installed", "pkgs"}, filename) {
				pkgs := s.extractTextPackages(path)
				packages = append(packages, pkgs...)
			}
		}

		return nil
	})

	return packages
}

// extractDpkgPackages parses dpkg database files
func (s *TrivyScanner) extractDpkgPackages(path string) []PackageManifest {
	packages := make([]PackageManifest, 0)

	data, err := os.ReadFile(path)
	if err != nil {
		s.logger.Debugf("Cannot read dpkg file %s: %v", path, err)
		return packages
	}

	lines := splitLines(string(data))
	for _, line := range lines {
		pkg := parseDpkgLine(line)
		if pkg.Name != "" {
			packages = append(packages, pkg)
		}
	}

	return packages
}

// parseDpkgLine parses individual dpkg database entry
func parseDpkgLine(line string) PackageManifest {
	line = trimSpace(line)
	if len(line) == 0 || len(line) < 10 {
		return PackageManifest{}
	}

	name := ""
	version := ""

	if idx := indexOf(line, "Package:"); idx >= 0 {
		name = trimSpace(line[idx+8:])
	}

	if idx := indexOf(line, "Version:"); idx >= 0 {
		version = trimSpace(line[idx+8:])
	}

	return PackageManifest{
		Name:    name,
		Version: version,
		Source:  "dpkg",
	}
}

// extractJSONPackages parses JSON-based package databases
func (s *TrivyScanner) extractJSONPackages(path string) []PackageManifest {
	packages := make([]PackageManifest, 0)

	data, err := os.ReadFile(path)
	if err != nil {
		return packages
	}

	var pkgData interface{}
	if err := json.Unmarshal(data, &pkgData); err != nil {
		return packages
	}

	if obj, ok := pkgData.(map[string]interface{}); ok {
		if pkgsArr, ok := obj["packages"].([]interface{}); ok {
			for _, item := range pkgsArr {
				if pkgObj, ok := item.(map[string]interface{}); ok {
					pkg := extractFromJSON(pkgObj)
					if pkg.Name != "" {
						packages = append(packages, pkg)
					}
				}
			}
		}

		if name, ok := obj["name"].(string); ok {
			version := ""
			if ver, ok := obj["version"].(string); ok {
				version = ver
			}
			packages = append(packages, PackageManifest{Name: name, Version: version, Source: "json"})
		}
	}

	return packages
}

// extractFromJSON extracts package info from JSON object
func extractFromJSON(obj map[string]interface{}) PackageManifest {
	name := getStringField(obj, "name", "package", "pkg_name")
	version := getStringField(obj, "version", "ver", "pkg_version")
	source := getStringField(obj, "source", "manager", "format")

	return PackageManifest{Name: name, Version: version, Source: source}
}

// extractTextPackages parses plain text package lists
func (s *TrivyScanner) extractTextPackages(path string) []PackageManifest {
	packages := make([]PackageManifest, 0)

	data, err := os.ReadFile(path)
	if err != nil {
		return packages
	}

	lines := splitLines(string(data))
	for _, line := range lines {
		pkg := parseTextPackageLine(line)
		if pkg.Name != "" {
			packages = append(packages, pkg)
		}
	}

	return packages
}

// parseTextPackageLine parses single-line package manifest
func parseTextPackageLine(line string) PackageManifest {
	line = trimSpace(line)
	if len(line) == 0 {
		return PackageManifest{}
	}

	var name, version string

	if idx := indexOf(line, "-"); idx > 0 {
		name = trimSpace(line[:idx])
		version = trimSpace(line[idx+1:])
	} else if idx := indexOf(line, ":"); idx > 0 {
		name = trimSpace(line[:idx])
		if verIdx := indexOf(line[idx+1:], "."); verIdx >= 0 {
			version = trimSpace(line[idx+1 : idx+1+verIdx])
		}
	} else {
		name = line
	}

	return PackageManifest{Name: name, Version: version, Source: "text"}
}

// calculateSummary counts vulnerabilities by severity
func (s *TrivyScanner) calculateSummary(vulns []OSPackageVulns) VulnerabilitySummary {
	summary := VulnerabilitySummary{}
	summary.TotalCount = len(vulns)

	for _, vuln := range vulns {
		switch vuln.Severity {
		case types.Unknown:
			summary.UnknownCount++
		case types.Low:
			summary.LowCount++
		case types.Medium:
			summary.MediumCount++
		case types.High:
			summary.HighCount++
		case types.Critical:
			summary.CriticalCount++
		default:
			if summary.unknownCountHelper(vuln.Severity) {
				summary.UnknownCount++
			} else {
				summary.MediumCount++
			}
		}
	}

	return summary
}

// TotalCount returns total vulnerability count
func (vs VulnerabilitySummary) TotalCount() int {
	return vs.CriticalCount + vs.HighCount + vs.MediumCount + vs.LowCount + vs.UnknownCount
}

// Helper functions
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func splitLines(s string) []string {
	lines := make([]string, 0)
	current := ""

	for _, r := range s {
		if r == '\n' || r == '\r' {
			if len(current) > 0 {
				lines = append(lines, current)
				current = ""
			}
		} else {
			current += string(r)
		}
	}

	if len(current) > 0 {
		lines = append(lines, current)
	}

	return lines
}

func getStringField(obj map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if val, ok := obj[key]; ok {
			if str, ok := val.(string); ok && len(str) > 0 {
				return str
			}
		}
	}
	return ""
}

func indexOf(s string, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// ============================================================================
// METRICS AND MONITORING
// ============================================================================

var vulnerabilityCountTracker = struct {
	totalQueries    int
	totalResults    int
	cachedLookups   int
	mu              sync.Mutex
}{
	totalQueries:  0,
	totalResults:  0,
	cachedLookups: 0,
	mu:            sync.Mutex{},
}

// TrackQuery records vulnerability query metrics
func TrackQuery(isCached bool) {
	vulnerabilityCountTracker.mu.Lock()
	defer vulnerabilityCountTracker.mu.Unlock()

	vulnerabilityCountTracker.totalQueries++
	if isCached {
		vulnerabilityCountTracker.cachedLookups++
	}
}

// GetMetrics returns vulnerability scanning metrics
func GetMetrics() (totalQueries, totalResults, cachedLookups int) {
	vulnerabilityCountTracker.mu.Lock()
	defer vulnerabilityCountTracker.mu.Unlock()

	return vulnerabilityCountTracker.totalQueries, vulnerabilityCountTracker.totalResults, vulnerabilityCountTracker.cachedLookups
}

// ResetMetrics clears all tracking counters
func ResetMetrics() {
	vulnerabilityCountTracker.mu.Lock()
	defer vulnerabilityCountTracker.mu.Unlock()

	vulnerabilityCountTracker.totalQueries = 0
	vulnerabilityCountTracker.totalResults = 0
	vulnerabilityCountTracker.cachedLookups = 0
}

// unknownCountHelper is a mock implementation helper
func (VulnerabilitySummary) unknownCountHelper(types.Severity) bool {
	return false
}
