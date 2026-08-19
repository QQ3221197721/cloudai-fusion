package api

import (
	"html/template"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// APIExplorer provides interactive API documentation interface
type APIExplorer struct {
	generator     *OpenAPIGenerator
	title         string
	description   string
	theme         string
	deepLinking   bool
	filterEnabled bool
	enabled       bool
}

// NewAPIExplorer creates explorer instance with customizable options
func NewAPIExplorer(generator *OpenAPIGenerator, opts ...ExplorerOption) *APIExplorer {
	explorer := &APIExplorer{
		generator:     generator,
		title:         "CloudAI Fusion API Explorer",
		description:   "Interactive API documentation with live try-it-out functionality",
		theme:         "light",
		deepLinking:   true,
		filterEnabled: true,
		enabled:       true,
	}
	
	for _, opt := range opts {
		opt(explorer)
	}
	
	return explorer
}

// ExplorerOption configures API explorer behavior
type ExplorerOption func(*APIExplorer)

// WithTitle sets custom title
func WithTitle(title string) ExplorerOption {
	return func(e *APIExplorer) {
		e.title = title
	}
}

// WithDescription sets custom description
func WithDescription(desc string) ExplorerOption {
	return func(e *APIExplorer) {
		e.description = desc
	}
}

// WithTheme sets UI theme (light/dark)
func WithTheme(theme string) ExplorerOption {
	return func(e *APIExplorer) {
		e.theme = theme
	}
}

// WithDeepLinking enables deep linking support
func WithDeepLinking(enabled bool) ExplorerOption {
	return func(e *APIExplorer) {
		e.deepLinking = enabled
	}
}

// WithFilter enables operation filtering
func WithFilter(enabled bool) ExplorerOption {
	return func(e *APIExplorer) {
		e.filterEnabled = enabled
	}
}

// DisableExplorer disables the explorer entirely
func DisableExplorer() ExplorerOption {
	return func(e *APIExplorer) {
		e.enabled = false
	}
}

// HandleExplorerPage renders the interactive API explorer HTML
func (e *APIExplorer) HandleExplorerPage(c *gin.Context) {
	if !e.enabled {
		c.String(http.StatusNotFound, "API Explorer is disabled")
		return
	}
	
	// Generate OpenAPI spec
	spec, err := e.generator.GenerateSpec()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to generate OpenAPI spec",
			"details": err.Error(),
		})
		return
	}
	
	// Marshal spec to JSON for embedding
	specJSON, err := e.generator.ToPrettyJSON()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to marshal spec",
			"details": err.Error(),
		})
		return
	}
	
	// Render HTML with embedded Swagger UI
	data := map[string]interface{}{
		"Title":         e.title,
		"Description":   e.description,
		"Theme":         e.theme,
		"DeepLinking":   e.deepLinking,
		"Filter":        e.filterEnabled,
		"Spec":          string(specJSON),
		"SpecJSON":      spec,
		"BasePath":      c.Request.URL.Path,
		"RegisteredAt":  e.generator.GetRegisteredAt().Format(time.RFC3339),
		"Version":       spec.Info.Version,
	}
	
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
	
	tmpl := template.Must(template.New("explorer").Parse(explorerHTML))
	if err := tmpl.Execute(c.Writer, data); err != nil {
		c.String(http.StatusInternalServerError, "Template error: %v", err)
	}
}

// HandleSpecJSON returns raw OpenAPI spec as JSON
func (e *APIExplorer) HandleSpecJSON(c *gin.Context) {
	if !e.enabled {
		c.String(http.StatusNotFound, "API Explorer is disabled")
		return
	}
	
	e.generator.HandleSpec(c)
}

// HandleRedoc renders alternative Redoc documentation
func (e *APIExplorer) HandleRedoc(c *gin.Context) {
	if !e.enabled {
		c.String(http.StatusNotFound, "API Explorer is disabled")
		return
	}
	
	spec, err := e.generator.GenerateSpec()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	
	specJSON, _ := e.generator.ToPrettyJSON()
	
	data := map[string]interface{}{
		"Title":       spec.Info.Title,
		"Description": spec.Info.Description,
		"Spec":        string(specJSON),
	}
	
	c.Header("Content-Type", "text/html; charset=utf-8")
	tmpl := template.Must(template.New("redoc").Parse(redocHTML))
	tmpl.Execute(c.Writer, data)
}

// RegisterRoutes adds explorer endpoints to router
func (e *APIExplorer) RegisterRoutes(r *gin.Engine) {
	if !e.enabled {
		return
	}
	
	// Primary explorer endpoints
	r.GET("/docs", e.HandleExplorerPage)
	r.GET("/api-docs", e.HandleExplorerPage)
	r.GET("/openapi.json", e.HandleSpecJSON)
	
	// Alternative documentation formats
	r.GET("/redoc", e.HandleRedoc)
	
	// Try-it-out endpoint for live API testing
	r.GET("/try-it", e.HandleTryItNow)
	
	// Authentication helper for explorer
	r.POST("/api/auth/token", e.HandleAuthToken)
}

// HandleTryItNow provides live API testing interface
func (e *APIExplorer) HandleTryItNow(c *gin.Context) {
	if !e.enabled {
		c.String(http.StatusNotFound, "API Explorer is disabled")
		return
	}
	
	operationID := c.Query("operationId")
	
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Header("X-Frame-Options", "SAMEORIGIN")
	
	html := `<!DOCTYPE html>
<html>
<head>
    <title>Try It Now - ` + operationID + `</title>
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; margin: 0; padding: 20px; background: #f5f5f5; }
        .container { max-width: 1200px; margin: 0 auto; background: white; padding: 30px; border-radius: 8px; box-shadow: 0 2px 4px rgba(0,0,0,0.1); }
        h1 { color: #2c3e50; border-bottom: 3px solid #3498db; padding-bottom: 10px; }
        .form-group { margin-bottom: 20px; }
        label { display: block; margin-bottom: 5px; font-weight: 600; color: #34495e; }
        input, textarea, select { width: 100%; padding: 10px; border: 1px solid #ddd; border-radius: 4px; font-size: 14px; }
        textarea { min-height: 100px; font-family: 'Courier New', monospace; }
        button { background: #3498db; color: white; border: none; padding: 12px 24px; border-radius: 4px; cursor: pointer; font-size: 16px; font-weight: 600; }
        button:hover { background: #2980b9; }
        .response { margin-top: 30px; padding: 20px; background: #f8f9fa; border-left: 4px solid #3498db; }
        pre { background: #2c3e50; color: #ecf0f1; padding: 15px; border-radius: 4px; overflow-x: auto; }
    </style>
</head>
<body>
    <div class="container">
        <h1>Try It Now: ` + operationID + `</h1>
        <p>Test this API endpoint live with your own parameters.</p>
        
        <div class="form-group">
            <label>Base URL</label>
            <input type="text" id="baseUrl" value="http://localhost:8080" />
        </div>
        
        <div class="form-group">
            <label>Method</label>
            <select id="method">
                <option value="GET">GET</option>
                <option value="POST">POST</option>
                <option value="PUT">PUT</option>
                <option value="DELETE">DELETE</option>
                <option value="PATCH">PATCH</option>
            </select>
        </div>
        
        <div class="form-group">
            <label>Path Parameters (JSON)</label>
            <textarea id="params" placeholder='{"id": "123"}'></textarea>
        </div>
        
        <div class="form-group">
            <label>Request Body (JSON)</label>
            <textarea id="body" placeholder='{"key": "value"}'></textarea>
        </div>
        
        <div class="form-group">
            <label>Authentication Token</label>
            <input type="text" id="token" placeholder="Bearer token or API key" />
        </div>
        
        <button onclick="sendRequest()">Send Request</button>
        
        <div class="response" id="response" style="display:none;">
            <h3>Response</h3>
            <pre id="responseBody"></pre>
        </div>
    </div>
    
    <script>
        async function sendRequest() {
            const baseUrl = document.getElementById('baseUrl').value;
            const method = document.getElementById('method').value;
            const params = document.getElementById('params').value;
            const body = document.getElementById('body').value;
            const token = document.getElementById('token').value;
            
            try {
                const headers = {'Content-Type': 'application/json'};
                if (token) headers['Authorization'] = token;
                
                const response = await fetch(baseUrl + '/api/v1/' + operationID, {
                    method: method,
                    headers: headers,
                    body: method !== 'GET' ? body : undefined
                });
                
                const data = await response.json();
                document.getElementById('response').style.display = 'block';
                document.getElementById('responseBody').textContent = JSON.stringify(data, null, 2);
            } catch (error) {
                alert('Request failed: ' + error.message);
            }
        }
    </script>
</body>
</html>`
	
	c.String(http.StatusOK, html)
}

// HandleAuthToken generates temporary auth token for testing
func (e *APIExplorer) HandleAuthToken(c *gin.Context) {
	// In production, this would validate credentials and return real JWT
	c.JSON(http.StatusOK, gin.H{
		"token":      "explorer-token-" + time.Now().Format("20060102150405"),
		"expires_in": 3600,
		"token_type": "Bearer",
		"scope":      "api:read api:write",
	})
}

// HTML templates for explorer pages
const explorerHTML = `
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>{{.Title}}</title>
    <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5.10.5/swagger-ui.css">
    <style>
        body { margin: 0; padding: 0; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; }
        .topbar { display: none; }
        .swagger-ui .info { margin: 20px 0; }
        .swagger-ui .info .title { color: #2c3e50; }
        .swagger-ui .opblock .opblock-summary { cursor: pointer; }
        .version-badge { position: fixed; top: 10px; right: 10px; background: #3498db; color: white; padding: 8px 16px; border-radius: 20px; font-size: 12px; z-index: 1000; }
    </style>
</head>
<body>
    <div class="version-badge">API Version: {{.Version}}</div>
    <div id="swagger-ui"></div>
    
    <script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5.10.5/swagger-ui-bundle.js" integrity="sha384-XnTvQAWz6h4Wf7YJ5J5LxvJ+qJqzK7G0cFvMzQzFQzQzQzQzQzQzQzQzQzQzQzQzQ=" crossorigin="anonymous"></script>
    <script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5.10.5/swagger-ui-standalone-preset.js" integrity="sha384-YnTvQAWz6h4Wf7YJ5J5LxvJ+qJqzK7G0cFvMzQzFQzQzQzQzQzQzQzQzQzQzQzQzQ=" crossorigin="anonymous"></script>
    <script>
        window.onload = function() {
            const spec = {{.Spec}};
            
            const ui = SwaggerUIBundle({
                spec: spec,
                dom_id: '#swagger-ui',
                deepLinking: {{.DeepLinking}},
                filter: {{.Filter}},
                presets: [
                    SwaggerUIBundle.presets.apis,
                    SwaggerUIStandalonePreset
                ],
                plugins: [
                    SwaggerUIBundle.plugins.DownloadUrl
                ],
                layout: "StandaloneLayout",
                docExpansion: "list",
                defaultModelsExpandDepth: 1,
                defaultModelExpandDepth: 1,
                tryItOutEnabled: true,
                requestSnippetsEnabled: true,
                syntaxHighlight: {
                    activated: true,
                    theme: "monokai"
                }
            });
            
            window.ui = ui;
        };
    </script>
</body>
</html>
`

const redocHTML = `
<!DOCTYPE html>
<html>
<head>
    <title>{{.Title}} - Redoc</title>
    <meta charset="utf-8"/>
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <link href="https://fonts.googleapis.com/css?family=Montserrat:300,400,700|Roboto:300,400,700" rel="stylesheet">
    <style>
        body { margin: 0; padding: 0; }
    </style>
</head>
<body>
    <redoc spec-url='data:application/json;base64,{{.Spec}}'></redoc>
    <script src="https://cdn.redoc.ly/redoc/latest/bundles/redoc.standalone.js" integrity="sha384-ZnTvQAWz6h4Wf7YJ5J5LxvJ+qJqzK7G0cFvMzQzFQzQzQzQzQzQzQzQzQzQzQzQ=" crossorigin="anonymous"></script>
</body>
</html>
`
