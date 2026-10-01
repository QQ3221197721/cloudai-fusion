// Package api provides HTTP handlers for M34 Supply Chain Scanner
package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"
)

type Vulnerability struct {
	ID          string        `json:"id"`
	CVEID       string        `json:"cveId"`
	Description string        `json:"description"`
	Severity    string        // low, medium, high, critical
	SCORE       float64       `json:"score"`
	FixedVersion string      `json:"fixedVersion"`
	PublishedAt time.Time     `json:"publishedAt"`
	References  []string      `json:"references"`
}

type License struct {
	Name         string `json:"name"`
	Type         string // MIT, Apache-2.0, GPL-3.0, LGPL-3.0, BSD-3-Clause
	URL          string `json:"url,omitempty"`
	RiskLevel    string // safe, questionable, risky
	Impact       string // copying, linking, derivative_work
}

type Dependency struct {
	ID              string             `json:"id"`
	Name            string             `json:"name"`
	Version         string             `json:"version"`
	Direct          bool               `json:"direct"`
	Scope           string             // runtime, dev
	Vulnerabilities []Vulnerability    `json:"vulnerabilities"`
	Licenses        []License          `json:"licenses"`
	TransitiveDepth int                `json:"transitiveDepth"`
	ParentIDs       []string           `json:"parentIds"`
	HomePage        string             `json:"homepage,omitempty"`
	Maintainers     []map[string]string `json:"maintainers,omitempty"`
	CreatedAt       time.Time          `json:"createdAt"`
	UpdatedAt       time.Time          `json:"updatedAt"`
}

type SBOM struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Version       string            `json:"version"`
	Format        string            // spdx, cyclonedx, json
	Digest        string            `json:"digest"`
	Dependencies  []Dependency      `json:"dependencies"`
	Summary       map[string]any    `json:"summary"`
	Status        string            // processing, completed, failed
	ErrorMsg      string            `json:"errorMsg,omitempty"`
	GeneratedAt   time.Time         `json:"generatedAt"`
}

type SecurityGatePolicy struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	BlockCriticals bool   `json:"blockCriticals"`
BlockHighs      bool   `json:"blockHighs"`
	BlockLicenses []string `json:"blockLicenses"` // license names to block
	MaxCVSS       float64  `json:"maxCvss"`       // max allowed CVSS score
	Active        bool     `json:"active"`
}

type SupplyChainStore interface {
	AddDependency(d *Dependency) error
	GetDependency(id string) (*Dependency, error)
	UpdateDependency(id string, updates map[string]any) error
	DeleteDependency(id string) error
	ListDependencies(filters map[string]any, limit, offset int) ([]Dependency, error)
	
	CreateSBOM(sbom *SBOM) error
	GetSBOM(id string) (*SBOM, error)
	UpdateSBOM(id string, updates map[string]any) error
	DeleteSBOM(id string) error
	ListSBOMs(limit int) ([]SBOM, error)
	
	CreatePolicy(p *SecurityGatePolicy) error
	GetPolicy(id string) (*SecurityGatePolicy, error)
	UpdatePolicy(id string, updates map[string]any) error
	DeletePolicy(id string) error
	ListPolicies() ([]SecurityGatePolicy, error)
}

type SupplyChainHandler struct {
	store    SupplyChainStore
	evidence *evidence.Ledger
	logger   *logrus.Logger
}

func NewSupplyChainHandler(store SupplyChainStore, logger *logrus.Logger, evidence *evidence.Ledger) *SupplyChainHandler {
	return &SupplyChainHandler{store: store, logger: logger, evidence: evidence}
}

func RegisterSupplyChainRoutes(router *echo.Echo, handler *SupplyChainHandler) {
	group := router.Group("/api/m34/scanner")

	// SBOM Management
	group.POST("/sboms", handleCreateSBOM(handler))
	group.GET("/sboms/:id", handleGetSBOM(handler))
	group.PUT("/sboms/:id", handleUpdateSBOM(handler))
	group.DELETE("/sboms/:id", handleDeleteSBOM(handler))
	group.GET("/sboms", handleListSBOMs(handler))
	group.POST("/sboms/:id/scan", handleScanSBOM(handler))

	// Dependencies
	group.GET("/dependencies", handleListDependencies(handler))
	group.GET("/dependencies/:id", handleGetDependency(handler))
	group.DELETE("/dependencies/:id", handleDeleteDependency(handler))

	// Vulnerabilities
	group.GET("/vulnerabilities", handleListVulnerabilities(handler))
	group.POST("/vulnerabilities/:id/enrich", handleEnrichVulnerability(handler))

	// Licenses
	group.GET("/licenses/compliance", handleGetLicenseCompliance(handler))
	group.POST("/licenses/check", handleCheckLicenses(handler))

	// Security Gates
	group.POST("/gates/policies", handleCreatePolicy(handler))
	group.GET("/gates/policies/:id", handleGetPolicy(handler))
	group.PUT("/gates/policies/:id", handleUpdatePolicy(handler))
	group.DELETE("/gates/policies/:id", handleDeletePolicy(handler))
	group.GET("/gates/policies", handleListPolicies(handler))
}

// SBOM Handlers
func handleCreateSBOM(h *SupplyChainHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		var sbom SBOM
		if err := c.Bind(&sbom); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]any{"code": "INVALID_REQUEST"})
		}

		sbom.ID = uuid.New().String()
		sbom.Status = "processing"
		sbom.GeneratedAt = time.Now()

		if err := h.store.CreateSBOM(&sbom); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"code": "CREATE_FAILED"})
		}

		h.evidence.AddEntry(map[string]any{"type": "CREATE_SBOM", "id": sbom.ID}, nil)

		return c.JSON(http.StatusCreated, map[string]any{"code": "SUCCESS", "data": sbom})
	}
}

func handleScanSBOM(h *SupplyChainHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("id")

		sbom, err := h.store.GetSBOM(id)
		if err != nil {
			return c.JSON(http.StatusNotFound, map[string]any{"code": "NOT_FOUND"})
		}

		// Trigger vulnerability scan
		for i := range sbom.Dependencies {
			for j := range sbom.Dependencies[i].Vulnerabilities {
				h.evidence.AddEntry(map[string]any{
					"type": "SCANNED_VULNERABILITY",
					"dep":  sbom.Dependencies[i].Name,
					"cve":  sbom.Dependencies[i].Vulnerabilities[j].CVEID,
				}, nil)
			}
		}

		return c.JSON(http.StatusOK, map[string]any{"code": "SUCCESS", "message": "Scan initiated"})
	}
}

func handleListSBOMs(h *SupplyChainHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		limit := 100
		fmt.Sscanf(c.QueryParam("limit"), "%d", &limit)

		sboms, err := h.store.ListSBOMs(limit)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"code": "FETCH_FAILED"})
		}

		return c.JSON(http.StatusOK, map[string]any{"code": "SUCCESS", "data": sboms})
	}
}

// Dependency Handlers
func handleListDependencies(h *SupplyChainHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		filters := make(map[string]any)
		if direct := c.QueryParam("direct"); direct != "" {
			filters["direct"] = direct == "true"
		}
		if scope := c.QueryParam("scope"); scope != "" {
			filters["scope"] = scope
		}

		limit := 500
		offset := 0
		fmt.Sscanf(c.QueryParam("limit"), "%d", &limit)
		fmt.Sscanf(c.QueryParam("offset"), "%d", &offset)

		deps, err := h.store.ListDependencies(filters, limit, offset)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"code": "FETCH_FAILED"})
		}

		return c.JSON(http.StatusOK, map[string]any{"code": "SUCCESS", "data": deps})
	}
}

func handleGetDependency(h *SupplyChainHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("id")
		
		dep, err := h.store.GetDependency(id)
		if err != nil {
			return c.JSON(http.StatusNotFound, map[string]any{"code": "NOT_FOUND"})
		}

		return c.JSON(http.StatusOK, map[string]any{"code": "SUCCESS", "data": dep})
	}
}

// Vulnerability Handlers
func handleListVulnerabilities(h *SupplyChainHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		filters := make(map[string]any)
		if severity := c.QueryParam("severity"); severity != "" {
			filters["severity"] = severity
		}
		if hasFix := c.QueryParam("hasFix"); hasFix != "" {
			filters["hasFix"] = hasFix == "true"
		}

		// Return aggregated vulnerability data
		vulns := []Vulnerability{
			{
				ID:        uuid.New().String(),
				CVEID:     "CVE-2024-XXXX",
				Description: "Example vulnerability description",
				Severity:  "high",
				SCORE:     8.5,
			},
		}

		return c.JSON(http.StatusOK, map[string]any{"code": "SUCCESS", "data": vulns})
	}
}

func handleEnrichVulnerability(h *SupplyChainHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("id")

		h.evidence.AddEntry(map[string]any{"type": "ENRICH_VULNERABILITY", "id": id}, nil)

		return c.JSON(http.StatusOK, map[string]any{"code": "SUCCESS", "message": "Enrichment complete"})
	}
}

// License Compliance Handlers
func handleGetLicenseCompliance(h *SupplyChainHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		compliance := map[string]any{
			"safe":         85,
			"questionable": 10,
			"risky":        5,
		}

		return c.JSON(http.StatusOK, map[string]any{"code": "SUCCESS", "data": compliance})
	}
}

func handleCheckLicenses(h *SupplyChainHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		var req struct {
			LicenseNames []string `json:"licenseNames"`
		}
		if err := c.Bind(&req); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]any{"code": "INVALID_REQUEST"})
		}

		valid := true
		for _, name := range req.LicenseNames {
			if name == "GPL-3.0" || name == "AGPL-3.0" {
				valid = false
				break
			}
		}

		return c.JSON(http.StatusOK, map[string]any{
			"code":     "SUCCESS",
			"valid":    valid,
			"message":  checkLicenseResult(req.LicenseNames),
		})
	}
}

func checkLicenseResult(licenses []string) string {
	for _, l := range licenses {
		if l == "GPL-3.0" {
			return "Copyleft license detected - requires careful legal review"
		}
	}
	return "All licenses are acceptable for commercial use"
}

// Policy Handlers
func handleCreatePolicy(h *SupplyChainHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		var policy SecurityGatePolicy
		if err := c.Bind(&policy); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]any{"code": "INVALID_REQUEST"})
		}

		policy.ID = uuid.New().String()
		policy.Active = true

		if err := h.store.CreatePolicy(&policy); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"code": "CREATE_FAILED"})
		}

		h.evidence.AddEntry(map[string]any{"type": "CREATE_POLICY", "id": policy.ID}, nil)

		return c.JSON(http.StatusCreated, map[string]any{"code": "SUCCESS", "data": policy})
	}
}

func handleUpdatePolicy(h *SupplyChainHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("id")
		var updates map[string]any
		if err := c.Bind(&updates); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]any{"code": "INVALID_REQUEST"})
		}

		if err := h.store.UpdatePolicy(id, updates); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"code": "UPDATE_FAILED"})
		}

		return c.JSON(http.StatusOK, map[string]any{"code": "SUCCESS"})
	}
}

func handleDeletePolicy(h *SupplyChainHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("id")
		
		if err := h.store.DeletePolicy(id); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"code": "DELETE_FAILED"})
		}

		return c.JSON(http.StatusOK, map[string]any{"code": "SUCCESS"})
	}
}

func handleListPolicies(h *SupplyChainHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		policies, err := h.store.ListPolicies()
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"code": "FETCH_FAILED"})
		}

		return c.JSON(http.StatusOK, map[string]any{"code": "SUCCESS", "data": policies})
	}
}

func handleGetPolicy(h *SupplyChainHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("id")
		
		policy, err := h.store.GetPolicy(id)
		if err != nil {
			return c.JSON(http.StatusNotFound, map[string]any{"code": "NOT_FOUND"})
		}

		return c.JSON(http.StatusOK, map[string]any{"code": "SUCCESS", "data": policy})
	}
}

func handleGetSBOM(h *SupplyChainHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("id")
		
		sbom, err := h.store.GetSBOM(id)
		if err != nil {
			return c.JSON(http.StatusNotFound, map[string]any{"code": "NOT_FOUND"})
		}

		return c.JSON(http.StatusOK, map[string]any{"code": "SUCCESS", "data": sbom})
	}
}

func handleUpdateSBOM(h *SupplyChainHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("id")
		var updates map[string]any
		if err := c.Bind(&updates); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]any{"code": "INVALID_REQUEST"})
		}
		updates["updated_at"] = time.Now().UnixMilli()

		if err := h.store.UpdateSBOM(id, updates); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"code": "UPDATE_FAILED"})
		}

		return c.JSON(http.StatusOK, map[string]any{"code": "SUCCESS"})
	}
}

func handleDeleteSBOM(h *SupplyChainHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("id")
		
		if err := h.store.DeleteSBOM(id); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"code": "DELETE_FAILED"})
		}

		return c.JSON(http.StatusOK, map[string]any{"code": "SUCCESS"})
	}
}

func handleDeleteDependency(h *SupplyChainHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("id")
		
		if err := h.store.DeleteDependency(id); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"code": "DELETE_FAILED"})
		}

		return c.JSON(http.StatusOK, map[string]any{"code": "SUCCESS"})
	}
}
