// Package cloudai_dashboard provides comprehensive Red Team web dashboard
package dashboard

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/sirupsen/logrus"
)

// ============================================================================
// Main Dashboard Server
// ============================================================================

// DashboardServer manages the entire Red Team control interface
type DashboardServer struct {
	logger     *logrus.Logger
	router     *mux.Router
	httpServer *http.Server
	staticDir  string
}

// NewDashboardServer creates dashboard instance
func NewDashboardServer(addr string, logger *logrus.Logger) *DashboardServer {
	if logger == nil {
		logger = logrus.New()
	}
	
	ds := &DashboardServer{
		logger:    logger.WithField("component", "dashboard_server"),
		router:    mux.NewRouter(),
		staticDir: "./static",
	}
	
	// Configure HTTP server
	ds.httpServer = &http.Server{
		Addr:         addr,
		Handler:      ds.router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	
	return ds
}

// Start launches the dashboard server
func (ds *DashboardServer) Start(ctx context.Context) error {
	ds.logger.Info("Starting CloudAI Fusion Red Team Dashboard...")
	
	// Register routes
	ds.registerRoutes()
	
	// Start server in goroutine
	go func() {
		ds.logger.Infof("Dashboard listening on %s", ds.httpServer.Addr)
		if err := ds.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			ds.logger.Fatalf("Dashboard server failed: %v", err)
		}
	}()
	
	<-ctx.Done()
	
	// Graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	return ds.httpServer.Shutdown(shutdownCtx)
}

// ============================================================================
// Route Registration
// ============================================================================

func (ds *DashboardServer) registerRoutes() {
	// Static files
	fs := http.FileServer(http.Dir(ds.staticDir))
	ds.router.PathPrefix("/static/").Handler(http.StripPrefix("/static/", fs))
	
	// API routes
	api := ds.router.PathPrefix("/api/v1").Subrouter()
	
	// Exploits
	api.HandleFunc("/exploits/list", ds.handleExploitList).Methods("GET")
	api.HandleFunc("/exploits/run", ds.handleRunExploit).Methods("POST")
	api.HandleFunc("/exploits/status/{cve_id}", ds.handleExploitStatus).Methods("GET")
	
	// ED Bypass
	api.HandleFunc("/edr-bypass/test", ds.handleEDRTest).Methods("POST")
	api.HandleFunc("/edr-bypass/results", ds.handleEDRResults).Methods("GET")
	
	// Kerberos
	api.HandleFunc("/kerberos/ticket/create", ds.handleCreateTicket).Methods("POST")
	api.HandleFunc("/kerberos/tickets/list", ds.handleListTickets).Methods("GET")
	
	// Integrations
	api.HandleFunc("/integrations/slack/test", ds.handleSlackTest).Methods("POST")
	api.HandleFunc("/integrations/jira/tickets", ds.handleJiraTickets).Methods("GET")
	
	// Dashboards
	api.HandleFunc("/dashboards/overview", ds.handleOverview).Methods("GET")
	api.HandleFunc("/dashboards/cves", ds.handleCVEDashboard).Methods("GET")
	api.HandleFunc("/dashboards/edr", ds.handleEDRDDashboard).Methods("GET")
	
	// Index page
	ds.router.Handle("/", http.HandlerFunc(ds.handleIndex)).Methods("GET")
}

// ============================================================================
// HTTP Handlers
// ============================================================================

func (ds *DashboardServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	tmpl := template.Must(template.ParseFiles("./templates/index.html"))
	data := map[string]interface{}{
		"title":     "CloudAI Fusion Red Team Dashboard",
		"timestamp": time.Now().Format("2006-01-02 15:04:05"),
	}
	
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tmpl.Execute(w, data)
}

func (ds *DashboardServer) handleExploitList(w http.ResponseWriter, r *http.Request) {
	ds.logger.Info("Listing available exploits...")
	
	response := map[string]interface{}{
		"status": "success",
		"exploits": []map[string]string{
			{"id": "CVE-2024-3091", "name": "XZ Utils Backdoor", "cvss": "9.8"},
			{"id": "CVE-2024-21412", "name": "Windows Print Spooler", "cvss": "7.8"},
			{"id": "CVE-2023-28868", "name": "Edge Sandbox Escape", "cvss": "9.8"},
			{"id": "CVE-2024-21626", "name": "Hyper-V Container", "cvss": "7.8"},
		},
	}
	
	jsonResponse(w, response)
}

func (ds *DashboardServer) handleRunExploit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CVEID       string `json:"cve_id"`
		Target      string `json:"target"`
		PayloadType string `json:"payload_type"`
	}
	
	if err := json.Decode(r.Body, &req); err != nil {
		jsonError(w, "Invalid request: "+err.Error())
		return
	}
	
	ds.logger.Printf("Running exploit %s against target %s...", req.CVEID, req.Target)
	
	// In production: execute exploit
	response := map[string]interface{}{
		"status":        "success",
		"job_id":        fmt.Sprintf("exploit-%d", time.Now().UnixNano()),
		"cve_id":        req.CVEID,
		"target":        req.Target,
		"estimated_time": "2-5 minutes",
	}
	
	jsonResponse(w, response)
}

func (ds *DashboardServer) handleExploitStatus(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	cveID := vars["cve_id"]
	
	ds.logger.Printf("Checking status for CVE %s...", cveID)
	
	response := map[string]interface{}{
		"cve_id":        cveID,
		"status":        "completed",
		"progress":      100,
		"result":        "success",
		"evidence":      []string{"shellcode_received", "privilege_escalated"},
		"execution_time": "00:02:34",
	}
	
	jsonResponse(w, response)
}

func (ds *DashboardServer) handleEDRTest(w http.ResponseWriter, r *http.Request) {
	ds.logger.Info("Starting EDR bypass test suite...")
	
	// Trigger PoC validation
	suite := edrbypass.NewEDRTestSuite(nil)
	
	ctx := context.Background()
	err := suite.RunAllTests(ctx)
	if err != nil {
		jsonError(w, "EDR test failed: "+err.Error())
		return
	}
	
	response := map[string]interface{}{
		"status":             "success",
		"tests_completed":    suite.GetTotalCount(),
		"tests_passed":       suite.GetSuccessCount(),
		"overall_success_rate": suite.GetSuccessRate(),
	}
	
	jsonResponse(w, response)
}

func (ds *DashboardServer) handleEDRResults(w http.ResponseWriter, r *http.Request) {
	ds.logger.Info("Fetching EDR test results...")
	
	suite := edrbypass.NewEDRTestSuite(nil)
	results := suite.GetAllResults()
	
	response := map[string]interface{}{
		"total_tests": len(results),
		"results":     results,
	}
	
	jsonResponse(w, response)
}

func (ds *DashboardServer) handleCreateTicket(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TicketType string `json:"ticket_type"` // golden or silver
		Username   string `json:"username"`
		Domain     string `json:"domain"`
	}
	
	if err := json.Decode(r.Body, &req); err != nil {
		jsonError(w, "Invalid request: "+err.Error())
		return
	}
	
	ds.logger.Printf("Creating %s ticket for user %s...", req.TicketType, req.Username)
	
	response := map[string]interface{}{
		"status":     "success",
		"ticket_id":  fmt.Sprintf("KRB-%d", time.Now().UnixNano()),
		"ticket_type": req.TicketType,
		"username":   req.Username,
		"domain":     req.Domain,
		"validity":   "24 hours",
	}
	
	jsonResponse(w, response)
}

func (ds *DashboardServer) handleListTickets(w http.ResponseWriter, r *http.Request) {
	ds.logger.Info("Listing issued tickets...")
	
	response := map[string]interface{}{
		"tickets": []map[string]string{
			{"id": "KRB-1722876543", "type": "golden", "user": "admin@cloudai.fusion", "expires": "2026-08-06 14:30"},
			{"id": "KRB-1722876789", "type": "silver", "user": "service@cloudai.fusion", "expires": "2026-08-06 15:00"},
		},
	}
	
	jsonResponse(w, response)
}

func (ds *DashboardServer) handleOverview(w http.ResponseWriter, r *http.Request) {
	ds.logger.Info("Generating overview dashboard data...")
	
	// Gather metrics from all subsystems
	metrics := MetricsCollector{}.GetAllMetrics()
	
	response := map[string]interface{}{
		"exploits_active":  metrics.ExploitsActive,
		"edr_success_rate": metrics.EDRSuccessRate,
		"tickets_issued":   metrics.TicketsIssued,
		"integration_status": map[string]string{
			"slack":  "connected",
			"github": "connected",
			"jira":   "connected",
		},
	}
	
	jsonResponse(w, response)
}

// ============================================================================
// Utility Functions
// ============================================================================

func jsonResponse(w http.ResponseWriter, data map[string]interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.Marshal(w, data)
}

func jsonError(w http.ResponseWriter, message string) {
	response := map[string]string{"error": message}
	jsonResponse(w, response)
}
