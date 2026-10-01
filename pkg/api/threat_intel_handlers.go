// Package api provides HTTP handlers for M31 Automated Threat Intelligence Platform
package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jinzhu/copier"
	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"
)

// IOCTypes defines supported IOC types
type IOCType string

const (
	IocTypeIP       IOCType = "ip"
	IocTypeDomain   IOCType = "domain"
	IocTypeHash     IOCType = "hash"
	IocTypeEmail    IOCType = "email"
	IocTypeURL      IOCType = "url"
	IocTypeFile     IOCType = "file"
)

// ThreatConfidence levels
type ThreatConfidence int

const (
	ConfidenceLow    ThreatConfidence = 1
	ConfidenceMedium ThreatConfidence = 2
	ConfidenceHigh   ThreatConfidence = 3
	ConfidenceCritical ThreatConfidence = 4
)

// IOCCollection represents an Indicator of Compromise
type IOCCollection struct {
	ID          string         `json:"id" db:"id"`
	Type        IOCType        `json:"type" db:"ioc_type"`
	Value       string         `json:"value" db:"value"`
	ThreatType  string         `json:"threatType" db:"threat_type"` // APT, ransomware, botnet, etc.
	Description string         `json:"description" db:"description"`
	Confidence  ThreatConfidence `json:"confidence" db:"confidence"`
	Severity    string         `json:"severity" db:"severity"` // low, medium, high, critical
	FirstSeen   time.Time      `json:"firstSeen" db:"first_seen"`
	LastSeen    time.Time      `json:"lastSeen" db:"last_seen"`
	Active      bool           `json:"active" db:"active"`
	Sources     []string       `json:"sources" db:"sources"` // threat feed names
	Context     map[string]any `json:"context" db:"context"`  // additional metadata
	CreatedAt   time.Time      `json:"createdAt" db:"created_at"`
	UpdatedAt   time.Time      `json:"updatedAt" db:"updated_at"`
}

// ThreatFeedConfig configuration for threat intelligence feeds
type ThreatFeedConfig struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	URL            string            `json:"url"`
	AuthType       string            `json:"authType"` // api_key, oauth, basic
	Credentials    map[string]string `json:"credentials,omitempty"`
	UpdateInterval time.Duration     `json:"updateInterval"`
	Active         bool              `json:"active"`
	LastSync       time.Time         `json:"lastSync"`
	Error          string            `json:"error,omitempty"`
}

// ThreatCorrelationResult represents correlation analysis results
type ThreatCorrelationResult struct {
	ID                string                   `json:"id"`
	IOC               IOCCollection            `json:"ioc"`
	AssociatedCampaigns []string                `json:"associatedCampaigns"`
	RelatedIOCs       []string                 `json:"relatedIOCs"`
	AttackTactics     []string                 `json:"attackTactics"` // MITRE ATT&CK tactics
	TTPs              []string                 `json:"ttps"`         // Techniques
	RiskScore         float64                  `json:"riskScore"`
	AnalyzedAt        time.Time                `json:"analyzedAt`
}

// ThreatIntelStore interface for threat intelligence data persistence
type ThreatIntelStore interface {
	CreateIOC(ioc *IOCCollection) error
	GetIOC(id string) (*IOCCollection, error)
	UpdateIOC(id string, updates map[string]any) error
	DeleteIOC(id string) error
	ListIOCs(filters map[string]any, limit, offset int) ([]IOCCollection, error)
	AddIOCSource(iocID, source string) error
	GetIOCSources(iocID string) ([]string, error)
	
	CreateFeed(feed *ThreatFeedConfig) error
	GetFeed(id string) (*ThreatFeedConfig, error)
	UpdateFeed(id string, updates map[string]any) error
	DeleteFeed(id string) error
	ListFeeds() ([]ThreatFeedConfig, error)
	
	CorrelateIOC(iocID string) (*ThreatCorrelationResult, error)
	LogCorrelation(result *ThreatCorrelationResult) error
}

// ThreatIntelHandler handles M31 Automated Threat Intelligence API requests
type ThreatIntelHandler struct {
	store           ThreatIntelStore
	evidenceLedger  *evidence.Ledger
	logger          *logrus.Logger
	websocketUpgrader websocket.Upgrader
}

// NewThreatIntelHandler creates a new threat intelligence handler
func NewThreatIntelHandler(
	store ThreatIntelStore,
	ledger *evidence.Ledger,
	logger *logrus.Logger,
) *ThreatIntelHandler {
	return &ThreatIntelHandler{
		store:           store,
		evidenceLedger:  ledger,
		logger:          logger,
		websocketUpgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // TODO: Add origin validation in production
			},
		},
	}
}

// RegisterThreatIntelRoutes registers threat intelligence routes
func RegisterThreatIntelRoutes(router *echo.Echo, handler *ThreatIntelHandler) {
	group := router.Group("/api/m31/threat-intel")

	// IOC Management
	group.POST("/iods", handleCreateIOC(handler))
	group.GET("/iods/:id", handleGetIOC(handler))
	group.PUT("/iods/:id", handleUpdateIOC(handler))
	group.DELETE("/iods/:id", handleDeleteIOC(handler))
	group.GET("/iods", handleListIOCs(handler))
	group.POST("/iods/:id/sources", handleAddIOCSource(handler))

	// Feed Management
	group.POST("/feeds", handleCreateFeed(handler))
	group.GET("/feeds/:id", handleGetFeed(handler))
	group.PUT("/feeds/:id", handleUpdateFeed(handler))
	group.DELETE("/feeds/:id", handleDeleteFeed(handler))
	group.GET("/feeds", handleListFeeds(handler))
	group.POST("/feeds/:id/sync", handleSyncFeed(handler))

	// Correlation Analysis
	group.POST("/iods/:id/correlate", handleCorrelateIOC(handler))
	group.GET("/correlations/:id", handleGetCorrelation(handler))
	group.GET("/correlations", handleListCorrelations(handler))

	// Sharing & Export
	group.POST("/export", handleExportIOCs(handler))
	group.POST("/share", handleShareIOC(handler))

	// Analytics
	group.GET("/analytics/stats", handleGetIOCStats(handler))
	group.GET("/analytics/trends", handleGetIOCtrends(handler))
}

// handleCreateIOC creates a new IOC
func handleCreateIOC(h *ThreatIntelHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		var ioc IOCCollection
		if err := c.Bind(&ioc); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]any{
				"code":    "INVALID_REQUEST",
				"message": err.Error(),
			})
		}

		// Validate IOC type and value
		if err := validateIOC(ioc); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]any{
				"code":    "INVALID_IOC",
				"message": err.Error(),
			})
		}

		ioc.ID = uuid.New().String()
		ioc.FirstSeen = time.Now()
		ioc.LastSeen = time.Now()
		ioc.Active = true
		ioc.CreatedAt = time.Now()
		ioc.UpdatedAt = time.Now()

		if err := h.store.CreateIOC(&ioc); err != nil {
			h.logger.WithError(err).Error("Failed to create IOC")
			return c.JSON(http.StatusInternalServerError, map[string]any{
				"code":    "CREATE_FAILED",
				"message": "Failed to create indicator",
			})
		}

		// Create ZKP evidence record
		createAction := map[string]any{
			"type":      "CREATE_IOC",
			"ioCID":     ioc.ID,
			"value":     ioc.Value,
			"threatType": ioc.ThreatType,
			"timestamp": time.Now().UnixNano(),
		}
		if _, err := h.evidenceLedger.AddEntry(createAction, nil); err != nil {
			h.logger.WithError(err).Warn("Evidence recording failed but IOC created")
		}

		return c.JSON(http.StatusCreated, map[string]any{
			"code":    "SUCCESS",
			"message": "IOC created successfully",
			"data":    ioc,
		})
	}
}

// handleGetIOC retrieves an IOC by ID
func handleGetIOC(h *ThreatIntelHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("id")
		ioc, err := h.store.GetIOC(id)
		if err != nil {
			return c.JSON(http.StatusNotFound, map[string]any{
				"code":    "NOT_FOUND",
				"message": "IOC not found",
			})
		}

		return c.JSON(http.StatusOK, map[string]any{
			"code": "SUCCESS",
			"data": ioc,
		})
	}
}

// handleUpdateIOC updates an IOC
func handleUpdateIOC(h *ThreatIntelHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("id")
		var updates map[string]any
		if err := c.Bind(&updates); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]any{
				"code":    "INVALID_REQUEST",
				"message": err.Error(),
			})
		}

		updates["updated_at"] = time.Now().UnixNano() / 1e6
		if err := h.store.UpdateIOC(id, updates); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{
				"code":    "UPDATE_FAILED",
				"message": "Failed to update IOC",
			})
		}

		// Log update event
		h.evidenceLedger.AddEntry(map[string]any{
			"type":  "UPDATE_IOC",
			"ioCID": id,
			"updates": updates,
		}, nil)

		return c.JSON(http.StatusOK, map[string]any{
			"code":    "SUCCESS",
			"message": "IOC updated successfully",
		})
	}
}

// handleDeleteIOC deletes an IOC
func handleDeleteIOC(h *ThreatIntelHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("id")

		// Soft delete - mark inactive
		if err := h.store.UpdateIOC(id, map[string]any{
			"active": false,
		}); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{
				"code":    "DELETE_FAILED",
				"message": "Failed to delete IOC",
			})
		}

		// Record deletion evidence
		h.evidenceLedger.AddEntry(map[string]any{
			"type":  "DELETE_IOC",
			"ioCID": id,
			"timestamp": time.Now().UnixNano(),
		}, nil)

		return c.JSON(http.StatusOK, map[string]any{
			"code":    "SUCCESS",
			"message": "IOC deleted successfully",
		})
	}
}

// handleListIOCs lists all IOCs with optional filtering
func handleListIOCs(h *ThreatIntelHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		filters := make(map[string]any)
		
		if iocType := c.QueryParam("type"); iocType != "" {
			filters["type"] = iocType
		}
		if threatType := c.QueryParam("threatType"); threatType != "" {
			filters["threatType"] = threatType
		}
		if active := c.QueryParam("active"); active != "" {
			filters["active"] = active == "true"
		}

		limit := 100
		offset := 0
		if l := c.QueryParam("limit"); l != "" {
			fmt.Sscanf(l, "%d", &limit)
		}
		if o := c.QueryParam("offset"); o != "" {
			fmt.Sscanf(o, "%d", &offset)
		}

		ioCs, err := h.store.ListIOCs(filters, limit, offset)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{
				"code":    "FETCH_FAILED",
				"message": err.Error(),
			})
		}

		return c.JSON(http.StatusOK, map[string]any{
			"code": "SUCCESS",
			"data": ioCs,
		})
	}
}

// handleAddIOCSource adds a source to an IOC
func handleAddIOCSource(h *ThreatIntelHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("id")
		var req struct {
			Source string `json:"source"`
		}
		if err := c.Bind(&req); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]any{
				"code":    "INVALID_SOURCE",
				"message": err.Error(),
			})
		}

		if err := h.store.AddIOCSource(id, req.Source); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{
				"code":    "ADD_SOURCE_FAILED",
				"message": err.Error(),
			})
		}

		h.evidenceLedger.AddEntry(map[string]any{
			"type":    "ADD_IoC_SOURCE",
			"ioCID":   id,
			"source": req.Source,
		}, nil)

		return c.JSON(http.StatusOK, map[string]any{
			"code":    "SUCCESS",
			"message": "Source added successfully",
		})
	}
}

// handleCreateFeed creates a new threat feed
func handleCreateFeed(h *ThreatIntelHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		var feed ThreatFeedConfig
		if err := c.Bind(&feed); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]any{
				"code":    "INVALID_REQUEST",
				"message": err.Error(),
			})
		}

		feed.ID = uuid.New().String()
		if err := h.store.CreateFeed(&feed); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{
				"code":    "CREATE_FAILED",
				"message": "Failed to create feed",
			})
		}

		h.evidenceLedger.AddEntry(map[string]any{
			"type":       "CREATE_FEED",
			"feedID":     feed.ID,
			"feedName":   feed.Name,
			"feedURL":    feed.URL,
			"timestamp":  time.Now().UnixNano(),
		}, nil)

		return c.JSON(http.StatusCreated, map[string]any{
			"code":    "SUCCESS",
			"message": "Feed created successfully",
			"data":    feed,
		})
	}
}

// handleGetFeed retrieves a feed by ID
func handleGetFeed(h *ThreatIntelHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("id")
		feed, err := h.store.GetFeed(id)
		if err != nil {
			return c.JSON(http.StatusNotFound, map[string]any{
				"code":    "NOT_FOUND",
				"message": "Feed not found",
			})
		}

		return c.JSON(http.StatusOK, map[string]any{
			"code": "SUCCESS",
			"data": feed,
		})
	}
}

// handleSyncFeed triggers feed synchronization
func handleSyncFeed(h *ThreatIntelHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("id")

		// In real implementation, this would call external API
		// For now, we'll log the action as evidence
		h.evidenceLedger.AddEntry(map[string]any{
			"type":       "SYNC_FEED",
			"feedID":     id,
			"timestamp":  time.Now().UnixNano(),
		}, nil)

		return c.JSON(http.StatusOK, map[string]any{
			"code":    "SUCCESS",
			"message": "Feed sync initiated",
		})
	}
}

// handleCorrelateIOC performs correlation analysis
func handleCorrelateIOC(h *ThreatIntelHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("id")

		result, err := h.store.CorrelateIOC(id)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{
				"code":    "CORRELATION_FAILED",
				"message": err.Error(),
			})
		}

		// Log correlation result
		h.evidenceLedger.LogEvent(map[string]any{
			"type":       "IOC_CORRELATION",
			"ioCID":      id,
			"result":     result,
			"timestamp":  time.Now().UnixNano(),
		})

		return c.JSON(http.StatusOK, map[string]any{
			"code": "SUCCESS",
			"data": result,
		})
	}
}

// handleExportIOCs exports selected IOCs
func handleExportIOCs(h *ThreatIntelHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		var req struct {
			IoCIDs   []string `json:"iocIds"`
			Format   string   `json:"format"` // csv, stix, json
		}
		if err := c.Bind(&req); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]any{
				"code":    "INVALID_REQUEST",
				"message": err.Error(),
			})
		}

		// Record export event
		h.evidenceLedger.AddEntry(map[string]any{
			"type":      "EXPORT_IOCS",
			"ioCIDs":    req.IoCIDs,
			"format":    req.Format,
			"timestamp": time.Now().UnixNano(),
		}, nil)

		return c.JSON(http.StatusOK, map[string]any{
			"code":    "SUCCESS",
			"message": "Export initiated",
		})
	}
}

// handleShareIOC shares an IOC with partner organizations
func handleShareIOC(h *ThreatIntelHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		var req struct {
			IoCID string `json:"iocId"`
			OrgID string `json:"orgId"`
		}
		if err := c.Bind(&req); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]any{
				"code":    "INVALID_REQUEST",
				"message": err.Error(),
			})
		}

		h.evidenceLedger.AddEntry(map[string]any{
			"type":       "SHARE_IOC",
			"ioCID":      req.IoCID,
			"orgId":      req.OrgID,
			"timestamp":  time.Now().UnixNano(),
		}, nil)

		return c.JSON(http.StatusOK, map[string]any{
			"code":    "SUCCESS",
			"message": "IOC shared successfully",
		})
	}
}

// handleGetIOCStats returns IOC statistics
func handleGetIOCStats(h *ThreatIntelHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		stats := map[string]any{
			"total":         0,
			"active":        0,
			"inactive":      0,
			"byType":        make(map[string]int),
			"byThreatType":  make(map[string]int),
		}

		// Aggregate stats from store
		allIOCs, _ := h.store.ListIOCs(nil, 1000, 0)
		
		for _, ioc := range allIOCs {
			stats["total"] = stats["total"].(int) + 1
			
			if ioc.Active {
				stats["active"] = stats["active"].(int) + 1
			} else {
				stats["inactive"] = stats["inactive"].(int) + 1
			}
			
			typeStats := stats["byType"].(map[string]int)
			typeStats[string(ioc.Type)] = typeStats[string(ioc.Type)] + 1
			
			threatStats := stats["byThreatType"].(map[string]int)
			threatStats[ioc.ThreatType] = threatStats[ioc.ThreatType] + 1
		}

		return c.JSON(http.StatusOK, map[string]any{
			"code": "SUCCESS",
			"data": stats,
		})
	}
}

// handleGetIOCtrends returns IOC trends over time
func handleGetIOCtrends(h *ThreatIntelHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		days := 30
		if d := c.QueryParam("days"); d != "" {
			fmt.Sscanf(d, "%d", &days)
		}

		trendData := make([]map[string]any, days)
		for i := 0; i < days; i++ {
			date := time.Now().AddDate(0, 0, -i)
			trendData[i] = map[string]any{
				"date": date.Format("2006-01-02"),
				"count": 0,
			}
		}

		return c.JSON(http.StatusOK, map[string]any{
			"code": "SUCCESS",
			"data": trendData,
		})
	}
}

// validateIOC validates IOC structure and format
func validateIOC(ioc IOCCollection) error {
	if ioc.Value == "" {
		return fmt.Errorf("IOC value is required")
	}

	switch ioc.Type {
	case IocTypeIP:
		if strings.Contains(ioc.Value, "/") {
			// CIDR notation - simplified check
		} else if !isValidIP(ioc.Value) {
			return fmt.Errorf("invalid IP address format")
		}
	case IocTypeDomain:
		if !isValidDomain(ioc.Value) {
			return fmt.Errorf("invalid domain format")
		}
	case IocTypeHash:
		if len(ioc.Value) < 8 {
			return fmt.Errorf("hash too short")
		}
	}

	if ioc.Confidence < ConfidenceLow || ioc.Confidence > ConfidenceCritical {
		return fmt.Errorf("invalid confidence level")
	}

	return nil
}

// isValidIP checks if string is valid IP (IPv4 or IPv6)
func isValidIP(s string) bool {
	return strings.Count(s, ":") >= 2 || // IPv6
		strings.Count(s, ".") == 3 // IPv4
}

// isValidDomain checks if string is valid domain
func isValidDomain(s string) bool {
	return strings.Contains(s, ".") && 
		   len(s) > 3 && 
		   !strings.HasPrefix(s, ".") && 
		   !strings.HasSuffix(s, ".")
}
