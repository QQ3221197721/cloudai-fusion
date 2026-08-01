package redteam

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// MULTI-SOURCE CVE FEED MANAGER - 多源 CVE 数据聚合器
// ============================================================================

// ExploitInfo contains exploit metadata from various sources
type ExploitInfo struct {
	Platform       string    `json:"platform"`
	Author         string    `json:"author"`
	PublishDate    time.Time `json:"publish_date"`
	URL            string    `json:"url"`
	ProofOfConcept bool      `json:"proof_of_concept"`
	Verified       bool      `json:"verified"`
}

// TechniqueLink represents MITRE ATT&CK mapping
type TechniqueLink struct {
	TacticID     string   `json:"tactic_id"`
	TacticName   string   `json:"tactic_name"`
	TechniqueID  string   `json:"technique_id,omitempty"`
	TechniqueName string  `json:"technique_name,omitempty"`
}

// ThreatIndicator provides threat intelligence enrichment
type ThreatIndicator struct {
	TLP              string   `json:"tlp_level"` // Red/Amber/Green
	ActiveCampaign  bool     `json:"active_campaign"`
	APTGroups        []string `json:"apt_groups,omitempty"`
	ExploitType      string   `json:"exploit_type,omitempty"`
}

// CVEItemWithEnrichment is an enriched CVE item with multiple source data
type CVEItemWithEnrichment struct {
	CVE             CVEItem        `json:"cve"`
	ExploitMetadata *ExploitInfo   `json:"exploit_metadata,omitempty"`
	Techniques      []TechniqueLink `json:"techniques,omitempty"`
	ThreatIntel     []ThreatIndicator `json:"threat_intel,omitempty"`
}

// NVDAPIResponse represents NVD API v2.0 response structure
type NVDAPIResponse struct {
	Vulnerabilities []NVDCVEItem `json:"vulnerabilities"`
	TotalCount      int64        `json:"total_count"`
}

type NVDCVEItem struct {
	CVEID          string        `json:"cveId"`
	Container      NVDContainer  `json:"container"`
	Cve            NVDCoreData   `json:"cve"`
	Metrics        NVDMetrics    `json:"metrics"`
	LastModified   time.Time     `json:"lastModified"`
}

type NVDContainer struct {
	MetaData    map[string]interface{} `json:"metaData"`
}

type NVDCoreData struct {
	Description   []string                 `json:"descriptions"`
	References    []NVDReference           `json:"references"`
	VendorProducts []string                `json:"vendorProducts"`
}

type NVDReference struct {
	URL       string   `json:"url"`
	Sources   []string `json:"sources"`
}

type NVDMetrics struct {
	CVSSV3_1      CVSSMetrics `json:"CVSSv3_1"`
	CPEScanning   string      `json:"CPEScanning"`
}
type MultiSourceFeedManager struct {
	logger      *logrus.Logger
	httpClients map[string]*http.Client
	cacheDir    string
	lastUpdate  time.Time
	mu          sync.RWMutex
}

// NewMultiSourceFeedManager creates a CVE feed manager with retry logic and fallbacks
func NewMultiSourceFeedManager(logger *logrus.Logger, cacheDir string) *MultiSourceFeedManager {
	return &MultiSourceFeedManager{
		logger: logger,
		httpClients: map[string]*http.Client{
			"nvd":       newRetryingClient(30 * time.Second),
			"exploitdb": newRetryingClient(20 * time.Second),
			"vulners":   newRetryingClient(30 * time.Second),
			"packetstorm": newRetryingClient(25 * time.Second),
		},
		cacheDir: cacheDir,
	}
}

// FetchAllCVEs aggregates CVE data from all configured sources with parallel fetching
func (mfs *MultiSourceFeedManager) FetchAllCVEs(ctx context.Context, limit int) ([]CVEItemWithEnrichment, error) {
	mfs.mu.Lock()
	if time.Since(mfs.lastUpdate).Hours() < 24 {
		mfs.mu.Unlock()
		// Return cached data if recent (will implement caching later)
		mfs.mu.Unlock()
	}
	mfs.mu.Unlock()

	var results []CVEItemWithEnrichment

	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	resultChan := make(chan CVEItemWithEnrichment, limit*3)
	errChan := make(chan error, 10)

	go mfs.fetchFromNVDAPI(ctx, limit/2, resultChan)
	go mfs.fetchFromExploitDB(ctx, limit/3, resultChan)
	go mfs.fetchFromVulnersAPI(ctx, limit/6, resultChan)
	go mfs.fetchFromPacketStorm(ctx, limit/10, resultChan)

	collected := 0
	for collected < limit {
		select {
		case item, ok := <-resultChan:
			if !ok {
				return results, nil
			}
			results = append(results, item)
			collected++

		case err := <-errChan:
			mfs.logger.WithError(err).Warn("Failed to fetch from one source")

		case <-ctx.Done():
			return results, ctx.Err()
		}
	}

	mfs.mu.Lock()
	mfs.lastUpdate = time.Now()
	mfs.mu.Unlock()

	return results, nil
}

// ============================================================================
// NVD API INTEGRATION
// ============================================================================

func (mfs *MultiSourceFeedManager) fetchFromNVDAPI(ctx context.Context, limit int, resultChan chan<- CVEItemWithEnrichment) {
	defer close(resultChan)

	apiKey := getEnvOrFallback("NVD_API_KEY", "")
	baseURL := "https://services.nvd.nist.gov/rest/json/cves/2.0"

	params := url.Values{}
	params.Set("startIndex", "0")
	params.Set("verbosity", "LONG")
	params.Set("resultsPerPage", fmt.Sprintf("%d", limit))

	fullURL := baseURL + "?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", fullURL, nil)
	if err != nil {
		errChan <- err
		return
	}

	if apiKey != "" {
		req.Header.Set("ApiKey", apiKey)
	}

	resp, err := mfs.httpClients["nvd"].Do(req)
	if err != nil {
		errChan <- err
		return
	}
	defer resp.Body.Close()

	// Validate response URL to prevent SSRF
	if resp.Request.URL != nil {
		if err := validateURL(resp.Request.URL); err != nil {
			errChan <- fmt.Errorf("SSRF protection triggered: %w", err)
			return
		}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		errChan <- err
		return
	}

	var nvdResponse NVDAPIResponse
	if err := json.Unmarshal(body, &nvdResponse); err != nil {
		errChan <- err
		return
	}

	for _, cve := range nvdResponse.Vulnerabilities {
		enriched := CVEItemWithEnrichment{
			CVE:             cveToCVEItem(cve),
			ExploitMetadata: nil,
			Techniques:      extractMitreATT&CK(nvdResponse.CpeScanning),
			ThreatIntel:     nil,
		}
		resultChan <- enriched
	}
}

// ============================================================================
// EXPLOIT-DB INTEGRATION
// ============================================================================

func (mfs *MultiSourceFeedManager) fetchFromExploitDB(ctx context.Context, limit int, resultChan chan<- CVEItemWithEnrichment) {
	defer close(resultChan)

	exploitListURL := "https://www.exploit-db.com/exploits"

	resp, err := mfs.httpClients["exploitdb"].Get(exploitListURL)
	if err != nil {
		errChan <- err
		return
	}
	defer resp.Body.Close()

	// Validate response URL to prevent SSRF
	if resp.Request.URL != nil {
		if err := validateURL(resp.Request.URL); err != nil {
			errChan <- fmt.Errorf("SSRF protection triggered: %w", err)
			return
		}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		errChan <- err
		return
	}

	exploits := parseExploitDBTable(body)

	for _, exploit := range exploits[:min(limit/3, len(exploits))] {
		cveItem := CVEItemWithEnrichment{
			CVE: CVEItem{
				CVE: CVEData{
					CVEID:     exploit.CVEID,
					Summary:   fmt.Sprintf("Exploit available: %s", exploit.Title),
					Published: &exploit.PublishDate,
				},
			},
			ExploitMetadata: &ExploitInfo{
				Platform:       exploit.Platform,
				Author:         exploit.Author,
				PublishDate:    exploit.PublishDate,
				URL:            exploit.URL,
				ProofOfConcept: true,
				Verified:       exploit.Verified,
			},
			Techniques:      nil,
			ThreatIntel:     nil,
		}
		resultChan <- cveItem
	}
}

// ============================================================================
// VULNERS API INTEGRATION
// ============================================================================

func (mfs *MultiSourceFeedManager) fetchFromVulnersAPI(ctx context.Context, limit int, resultChan chan<- CVEItemWithEnrichment) {
	defer close(resultChan)

	apiKey := getEnvOrFallback("VULNERS_API_KEY", "")
	if apiKey == "" {
		mfs.logger.Warn("VULNERS_API_KEY not set, skipping Vulners integration")
		return
	}

	baseURL := "https://vulners.com/api/v3"

	// Search latest vulnerabilities
	searchQuery := map[string]interface{}{
		"query": map[string]interface{}{
			"bool": map[string]interface{}{
				"must": []map[string]interface{}{
					{"match": map[string]interface{}{"cve.cvelist.vdrift": 1}},
				},
			},
		},
		"size": limit,
	}

	queryBytes, _ := json.Marshal(searchQuery)

	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/search/all", strings.NewReader(string(queryBytes)))
	if err != nil {
		errChan <- err
		return
	}

	req.Header.Set("Authorization", apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := mfs.httpClients["vulners"].Do(req)
	if err != nil {
		errChan <- err
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		errChan <- err
		return
	}

	var vulnersResponse map[string]interface{}
	if err := json.Unmarshal(body, &vulnersResponse); err != nil {
		errChan <- err
		return
	}

	hits := vulnersResponse["hits"].([]interface{})
	for _, hit := range hits[:min(limit/6, len(hits))] {
		hitMap := hit.(map[string]interface{})
		
		cveID := ""
		if doc, ok := hitMap["document"].(map[string]interface{}); ok {
			if id, ok := doc["cveId"].(string); ok {
				cveID = id
			}
		}

		enriched := CVEItemWithEnrichment{
			CVE:             extractCVEFromVulners(hitMap),
			ExploitMetadata: extractExploitMetadataFromVulners(hitMap),
			Techniques:      extractTechniquesFromVulners(hitMap),
			ThreatIntel:     extractThreatIndicatorsFromVulners(hitMap),
		}

		resultChan <- enriched
	}
}

// ============================================================================
// PACKET STORM INTEGRATION
// ============================================================================

func (mfs *MultiSourceFeedManager) fetchFromPacketStorm(ctx context.Context, limit int, resultChan chan<- CVEItemWithEnrichment) {
	defer close(resultChan)

	feedsURL := "https://packetstormsecurity.com/files/"

	resp, err := mfs.httpClients["packetstorm"].Get(feedsURL)
	if err != nil {
		errChan <- err
		return
	}
	defer resp.Body.Close()

	// Validate response URL to prevent SSRF
	if resp.Request.URL != nil {
		if err := validateURL(resp.Request.URL); err != nil {
			errChan <- fmt.Errorf("SSRF protection triggered: %w", err)
			return
		}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		errChan <- err
		return
	}

	alerts := parsePacketStormAlerts(body)

	for _, alert := range alerts[:min(limit/10, len(alerts))] {
		cveItem := CVEItemWithEnrichment{
			CVE: CVEItem{
				CVE: CVEData{
					CVEID:     alert.CVEID,
					Summary:   fmt.Sprintf("Security Alert: %s", alert.Title),
					Published: &alert.PublishDate,
				},
			},
			ExploitMetadata: &ExploitInfo{
				Platform:      "various",
				Author:        alert.Author,
				PublishDate:   alert.PublishDate,
				URL:           alert.URL,
				ProofOfConcept: false,
				Verified:      false,
			},
			Techniques: nil,
		}
		resultChan <- cveItem
	}
}

// ============================================================================
// SECURITY: Network allowlist and SSRF protection
// ============================================================================

var allowedDomains = map[string]bool{
	"services.nvd.nist.gov":     true,
	"www.exploit-db.com":        true,
	"vulners.com":               true,
	"packetstormsecurity.com":   true,
}

func validateURL(u *url.URL) error {
	if !allowedDomains[u.Hostname()] {
		return fmt.Errorf("domain %s not in allowlist", u.Hostname())
	}

	host := u.Hostname()
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("DNS lookup failed: %w", err)
	}

	for _, ip := range ips {
		if isPrivateIP(ip) {
			return fmt.Errorf("private IP address %s blocked for security", ip)
		}
	}

	return nil
}

func isPrivateIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}

	if ip4 := ip.To4(); ip4 != nil {
		if ip4[0] == 10 || // 10.0.0.0/8
			(ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31) || // 172.16.0.0/12
			(ip4[0] == 192 && ip4[1] == 168) || // 192.168.0.0/16
			(ip4[0] == 127) { // 127.0.0.0/8
			return true
		}
	}

	return false
}

func newRetryingClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &RetryableTransport{
			MaxRetries: 3,
			BaseDelay:  1 * time.Second,
		},
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func getEnvOrFallback(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

// ============================================================================
// HELPER FUNCTIONS FOR VULNERS PARSING
// ============================================================================

func getFieldValue(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		} else if f, ok := v.(float64); ok {
			return fmt.Sprintf("%.1f", f)
		}
	}
	return ""
}

func extractReferencesFromVulners(doc map[string]interface{}) []Ref {
	var refs []Ref

	if refsRaw, ok := doc["refs"]; ok {
		if refsArr, ok := refsRaw.([]interface{}); ok {
			for _, r := range refsArr {
				if rMap, ok := r.(map[string]interface{}); ok {
					url := getFieldValue(rMap, "url")
					refs = append(refs, Ref{URL: url})
				}
			}
		}
	}

	return refs
}

func getStatusFromVulners(doc map[string]interface{}) string {
	if statusRaw, ok := doc["status"]; ok {
		if status, ok := statusRaw.(string); ok {
			return status
		}
	}
	return "Published"
}

func getImpactFromVulners(doc map[string]interface{}) ImpactScore {
	impact := ImpactScore{}

	if metricsRaw, ok := doc["cvss"]; ok {
		if metrics, ok := metricsRaw.(map[string]interface{}); ok {
			if vectorRaw, ok := metrics["vectorString"]; ok {
				if vector, ok := vectorRaw.(string); ok {
					impact.VectorString = vector
				}
			}
		}
	}

	return impact
}

func getSeverityFromScore(score float32) string {
	switch {
	case score >= 9.0:
		return "CRITICAL"
	case score >= 7.0:
		return "HIGH"
	case score >= 4.0:
		return "MEDIUM"
	default:
		return "LOW"
	}
}

func getVectorStringFromVulners(doc map[string]interface{}) string {
	if cvssRaw, ok := doc["cvss"]; ok {
		if cvss, ok := cvssRaw.(map[string]interface{}); ok {
			if vectorRaw, ok := cvss["vectorString"]; ok {
				if v, ok := vectorRaw.(string); ok {
					return v
				}
			}
		}
	}
	return ""
}

// ============================================================================
// DATA EXTRACTION HELPERS
// ============================================================================

func cveToCVEItem(nvdCve NVDCVEItem) CVEItem {
	// Extract description (usually first language is English)
	description := ""
	if len(nvdCve.Cve.Descriptions) > 0 {
		description = nvdCve.Cve.Descriptions[0].Value
	}

	return CVEItem{
		ID: nvdCve.CVEID,
		CVE: CVEData{
			Description: nvdCve.Cve.Descriptions,
			References:  nil, // Will be populated separately
		},
		Impact: ImpactScore{
			BaseScore:     0.0,
			BaseSeverity:  "", // Will be parsed from vector string
			VectorString:  "",
		},
		References: nil,
	}
}

func extractMitreATT&CK(cps string) []TechniqueLink {
	// Placeholder - would integrate with Vulners/MITRE APIs
	return make([]TechniqueLink, 0)
}

func parseExploitDBTable(body []byte) []ExploitEntry {
	// Simplified parser - would use HTML parsing library in production
	// This extracts CVE IDs, titles, platforms from Exploit-DB table rows
	return make([]ExploitEntry, 0)
}

type ExploitEntry struct {
	CVEID     string
	Title     string
	Platform  string
	Author    string
	PublishDate time.Time
	URL       string
	Verified  bool
}

func extractCVEFromVulners(hitMap interface{}) CVEItem {
	if hitMap == nil {
		return CVEItem{}
	}

	hit, ok := hitMap.(map[string]interface{})
	if !ok {
		return CVEItem{}
	}

	// Extract document field
	docRaw, ok := hit["document"]
	if !ok {
		return CVEItem{}
	}

	doc, ok := docRaw.(map[string]interface{})
	if !ok {
		return CVEItem{}
	}

	cveID := ""
	if id, ok := doc["cveId"]; ok {
		if str, ok := id.(string); ok {
			cveID = str
		}
	}

	var descriptions []string
	if descRaw, ok := doc["description"]; ok {
		if descStr, ok := descRaw.(string); ok {
			descriptions = append(descriptions, descStr)
		} else if descArr, ok := descRaw.([]interface{}); ok {
			for _, d := range descArr {
				if ds, ok := d.(string); ok {
					descriptions = append(descriptions, ds)
				}
			}
		}
	}

	cvssScore := float32(0.0)
	if metricsRaw, ok := doc["cvss"]; ok {
		if metrics, ok := metricsRaw.(map[string]interface{}); ok {
			if scoreRaw, ok := metrics["score"]; ok {
				switch v := scoreRaw.(type) {
				case float64:
					cvssScore = float32(v)
				case int:
					cvssScore = float32(v)
				case int64:
					cvssScore = float32(v)
				}
			}
		}
	}

	publishedTime := time.Now()
	if pubRaw, ok := doc["published"]; ok {
		if pubStr, ok := pubRaw.(string); ok {
			if t, err := time.Parse("2006-01-02", pubStr); err == nil {
				publishedTime = t
			}
		}
	}

	return CVEItem{
		ID: cveID,
		CVE: CVEData{
			Description:   descriptions,
			References:    extractReferencesFromVulners(doc),
			VulnStatus:    getStatusFromVulners(doc),
			Impact:        extractImpactFromVulners(doc),
		},
		Impact: ImpactScore{
			BaseScore:     cvssScore,
			BaseSeverity:  getSeverityFromScore(cvssScore),
			VectorString:  getVectorStringFromVulners(doc),
		},
		References: extractReferencesFromVulners(doc),
	}
}

func extractExploitMetadataFromVulners(hitMap interface{}) *ExploitInfo {
	if hitMap == nil {
		return nil
	}

	hit, ok := hitMap.(map[string]interface{})
	if !ok {
		return nil
	}

	docRaw, ok := hit["document"]
	if !ok {
		return nil
	}

	doc, ok := docRaw.(map[string]interface{})
	if !ok {
		return nil
	}

	// Check for exploits array
	exploitsRaw, ok := doc["exploits"]
	if !ok {
		return nil
	}

	exploits, ok := exploitsRaw.([]interface{})
	if !ok || len(exploits) == 0 {
		return nil
	}

	// Take first exploit as primary
	firstExploit, ok := exploits[0].(map[string]interface{})
	if !ok {
		return nil
	}

	platform := ""
	if p, ok := firstExploit["platform"]; ok {
		if ps, ok := p.(string); ok {
			platform = ps
		}
	}

	url := ""
	if u, ok := firstExploit["url"]; ok {
		if us, ok := u.(string); ok {
			url = us
		}
	}

	author := "Unknown"
	if authorRaw, ok := firstExploit["author"]; ok {
		if authorStr, ok := authorRaw.(string); ok {
			author = authorStr
		}
	}

	publishDate := time.Now()
	if dateRaw, ok := firstExploit["date"]; ok {
		if dateStr, ok := dateRaw.(string); ok {
			if t, err := time.Parse("2006-01-02", dateStr); err == nil {
				publishDate = t
			}
		}
	}

	return &ExploitInfo{
		Platform:       platform,
		Author:         author,
		PublishDate:    publishDate,
		URL:            url,
		ProofOfConcept: true,
		Verified:       false, // Would need to check exploit verification status
	}
}

func extractTechniquesFromVulners(hitMap interface{}) []TechniqueLink {
	if hitMap == nil {
		return make([]TechniqueLink, 0)
	}

	hit, ok := hitMap.(map[string]interface{})
	if !ok {
		return make([]TechniqueLink, 0)
	}

	docRaw, ok := hit["document"]
	if !ok {
		return make([]TechniqueLink, 0)
	}

	doc, ok := docRaw.(map[string]interface{})
	if !ok {
		return make([]TechniqueLink, 0)
	}

	var techniques []TechniqueLink

	// Try MITRE technique mapping
	mitreRaw, ok := doc["mitre"]
	if ok {
		if mitre, ok := mitreRaw.(map[string]interface{}); ok {
			if attacksRaw, ok := mitre["attacks"]; ok {
				if attacks, ok := attacksRaw.([]interface{}); ok {
					for _, attack := range attacks {
						if attackMap, ok := attack.(map[string]interface{}); ok {
							tactic := getFieldValue(attackMap, "tactic")
							technique := getFieldValue(attackMap, "technique")
							id := getFieldValue(attackMap, "id")

							techniques = append(techniques, TechniqueLink{
								TacticName:  tactic,
								TechniqueID: id,
								TechniqueName: technique,
							})
						}
					}
				}
			}
		}
	}

	// Also try direct techniques field
	if techniquesRaw, ok := doc["techniques"]; ok {
		if techniquesArr, ok := techniquesRaw.([]interface{}); ok {
			for _, tech := range techniquesArr {
				if techMap, ok := tech.(map[string]interface{}); ok {
					name := getFieldValue(techMap, "name")
					id := getFieldValue(techMap, "id")
					techniques = append(techniques, TechniqueLink{
						TechniqueID:   id,
						TechniqueName: name,
					})
				}
			}
		}
	}

	return techniques
}

func extractThreatIndicatorsFromVulners(hitMap interface{}) []ThreatIndicator {
	if hitMap == nil {
		return make([]ThreatIndicator, 0)
	}

	hit, ok := hitMap.(map[string]interface{})
	if !ok {
		return make([]ThreatIndicator, 0)
	}

	docRaw, ok := hit["document"]
	if !ok {
		return make([]ThreatIndicator, 0)
	}

	doc, ok := docRaw.(map[string]interface{})
	if !ok {
		return make([]ThreatIndicator, 0)
	}

	var indicators []ThreatIndicator

	// Check for campaigns (active exploitation)
	campaignsRaw, ok := doc["campaigns"]
	if ok {
		if campaigns, ok := campaignsRaw.([]interface{}); ok {
			for _, campaign := range campaigns {
				if campMap, ok := campaign.(map[string]interface{}); ok {
					tlp := getFieldValue(campMap, "tlp")
					aptGroup := getFieldValue(campMap, "group")

					indicators = append(indicators, ThreatIndicator{
						TLP:              tlp,
						ActiveCampaign:   true,
						APTGroups:        []string{aptGroup},
						ExploitType:      getFieldValue(campMap, "type"),
					})
				}
			}
		}
	}

	// Check for APT groups directly
	if aptRaw, ok := doc["apt"]; ok {
		if aptArr, ok := aptRaw.([]interface{}); ok {
			var aptGroups []string
			for _, apt := range aptArr {
				if aptStr, ok := apt.(string); ok {
					aptGroups = append(aptGroups, aptStr)
				}
			}
			indicators = append(indicators, ThreatIndicator{
				APTGroups: aptGroups,
			})
		}
	}

	return indicators
}

func parsePacketStormAlerts(body []byte) []SecurityAlert {
	// Placeholder - in production use html/parser or rss/atom parser
	return make([]SecurityAlert, 0)
}

// ============================================================================
// EXPLOIT-DB HTML PARSER
// ============================================================================

import (
	"html"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func parseExploitDBTable(body []byte) []ExploitEntry {
	// Use html package to parse HTML table
	htmlStr := html.UnescapeString(string(body))

	// Extract CVE IDs using regex pattern
	cvePattern := regexp.MustCompile(`CVE-[0-9]{4}-[0-9]+`)
	titlePattern := regexp.MustCompile(`<h[1-3]>.*?</h[1-3]>`)
	datePattern := regexp.MustCompile(`(\d{4}-\d{2}-\d{2}|\w+ \d+, \d+)`)

	exploits := make([]ExploitEntry, 0)

	// Simple extraction from raw HTML (would need proper HTML parser for robust implementation)
	cveMatches := cvePattern.FindAllString(htmlStr, -1)
	titleMatches := titlePattern.FindAllString(htmlStr, -1)

	// Match CVEs with titles (heuristic matching)
	for i, cveID := range cveMatches {
		if i < len(titleMatches) {
			title := strings.Trim(titleMatches[i], "<h2>")
			title = strings.ReplaceAll(title, "</h2>", "")

			exploit := ExploitEntry{
				CVEID:    cveID,
				Title:    title,
				Platform: "Various",
				Author:   "Unknown",
				URL:      fmt.Sprintf("https://www.exploit-db.com/exploits/%s", strconv.Itoa(i)),
			}
			exploits = append(exploits, exploit)
		}
	}

	return exploits
}

type SecurityAlert struct {
	CVEID     string
	Title     string
	Author    string
	PublishDate time.Time
	URL       string
}

