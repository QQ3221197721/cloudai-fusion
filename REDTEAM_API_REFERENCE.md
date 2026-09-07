# CloudAI Fusion Red Team Platform - API Reference

**Version**: v1.0.0  
**Last Updated**: September 2026  
**Base URL**: `http://localhost:8080/api/v1`

---

## Table of Contents

1. [Authentication](#authentication)
2. [Dashboard Statistics](#dashboard-statistics)
3. [Work Order Management](#work-order-management)
4. [Attack Campaigns](#attack-campaigns)
5. [Vulnerability Findings](#vulnerability-findings)
6. [Error Handling](#error-handling)
7. [SDK Examples](#sdk-examples)

---

## Authentication

The Red Team platform uses **JWT (JSON Web Token)** authentication for all protected endpoints.

### POST /api/v1/auth/login

Authenticate user and receive access/refresh tokens.

**Request Body**:
```json
{
  "username": "admin",
  "password": "strong-password"
}
```

**Success Response** (200 OK):
```json
{
  "access_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "token_type": "Bearer",
  "expires_in": 3600,
  "refresh_token": "dGhpcyBpcyBhIHJlZnJlc2ggdG9rZW4...",
  "user": {
    "id": 1,
    "username": "admin",
    "email": "admin@example.com",
    "roles": ["redteam_admin"]
  }
}
```

**Error Responses**:
- **401 Unauthorized**: Invalid credentials
  ```json
  {"error": "invalid credentials"}
  ```
- **400 Bad Request**: Missing username or password
  ```json
  {"error": "Key: 'Username' Error:Field validation for 'Username' on 'required'"}
  ```

### POST /api/v1/auth/refresh

Refresh access token using refresh token.

**Request Body**:
```json
{
  "refresh_token": "dGhpcyBpcyBhIHJlZnJlc2ggdG9rZW4..."
}
```

**Success Response** (200 OK):
```json
{
  "access_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "token_type": "Bearer",
  "expires_in": 3600,
  "refresh_token": "new-refresh-token-here..."
}
```

**Error Responses**:
- **401 Unauthorized**: Invalid or expired refresh token
  ```json
  {"error": "invalid token"}
  ```

### Authorization Header Format

Include JWT in all protected requests:

```bash
Authorization: Bearer <access_token>
```

**Example with curl**:
```bash
curl -X GET \
  http://localhost:8080/api/v1/stats/dashboard \
  -H "Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
```

---

## Dashboard Statistics

### GET /api/v1/stats/dashboard

Retrieve comprehensive dashboard metrics.

**Response** (200 OK):
```json
{
  "total_campaigns": 42,
  "active_campaigns": 7,
  "completed_campaigns": 35,
  "total_findings": 156,
  "critical_findings": 12,
  "high_findings": 34,
  "medium_findings": 67,
  "low_findings": 43,
  "last_scan_time": "2024-09-05T14:30:00Z",
  "coverage_percentage": 87.5
}
```

### GET /api/v1/stats/findings-by-severity

Get findings distribution by severity level.

**Response** (200 OK):
```json
[
  {
    "severity": "critical",
    "count": 12,
    "percentage": 7.7,
    "trend": "+2"
  },
  {
    "severity": "high",
    "count": 34,
    "percentage": 21.8,
    "trend": "-1"
  },
  {
    "severity": "medium",
    "count": 67,
    "percentage": 42.9,
    "trend": "+5"
  },
  {
    "severity": "low",
    "count": 43,
    "percentage": 27.6,
    "trend": "+3"
  }
]
```

### GET /api/v1/stats/recent-activity

Retrieve recent security activities.

**Query Parameters**:
- `limit` (optional): Number of records (default: 10, max: 100)
- `type` (optional): Filter by activity type (`campaign`, `finding`, `scan`)

**Response** (200 OK):
```json
[
  {
    "id": 123,
    "type": "campaign_completed",
    "title": "Web Application Security Scan Completed",
    "timestamp": "2024-09-05T14:30:00Z",
    "description": "Found 5 vulnerabilities in target example.com",
    "severity": "high",
    "related_entity": {
      "type": "campaign",
      "id": 456,
      "name": "example-com-web-scan"
    }
  },
  {
    "id": 124,
    "type": "finding_created",
    "title": "Critical SQL Injection Detected",
    "timestamp": "2024-09-05T13:45:00Z",
    "description": "SQL injection vulnerability in login form",
    "severity": "critical",
    "related_entity": {
      "type": "finding",
      "id": 789,
      "title": "SQL Injection in Login Form"
    }
  }
]
```

### GET /api/v1/stats/security-metrics

Get security performance metrics.

**Response** (200 OK):
```json
{
  "average_time_to_detect": "2.3 hours",
  "average_time_to_remEDIATE": "18.5 hours",
  "findings_per_day": 12.4,
  "false_positive_rate": 3.2,
  "coverage_score": 87.5,
  "remediation_rate": 94.2,
  "repeat_vuln_rate": 5.8,
  "top_attack_vectors": [
    {
      "vector": "OWASP Top 10 A01",
      "count": 45,
      "percentage": 28.9
    },
    {
      "vector": "MITRE ATT&CK T1566",
      "count": 34,
      "percentage": 21.8
    },
    {
      "vector": "CWE-89 (SQL Injection)",
      "count": 28,
      "percentage": 18.0
    }
  ],
  "monthly_trend": {
    "month": "September 2024",
    "findings_detected": 156,
    "findings_remediated": 147,
    "trend_direction": "improving"
  }
}
```

---

## Work Order Management

Work orders track remediation tasks created from security findings.

### POST /api/v1/work-orders

Create a new work order.

**Request Body**:
```json
{
  "title": "Patch Critical SQL Injection Vulnerability",
  "description": "Update application to fix SQL injection found in login form",
  "severity": "critical",
  "priority": "P1",
  "finding_id": 789,
  "assigned_team": "backend-dev",
  "deadline": "2024-09-10T23:59:59Z",
  "tags": ["security", "sql-injection", "p1-priority"]
}
```

**Success Response** (201 Created):
```json
{
  "id": 1001,
  "title": "Patch Critical SQL Injection Vulnerability",
  "status": "pending",
  "severity": "critical",
  "priority": "P1",
  "finding_id": 789,
  "assigned_team": "backend-dev",
  "deadline": "2024-09-10T23:59:59Z",
  "created_at": "2024-09-05T14:30:00Z",
  "updated_at": "2024-09-05T14:30:00Z"
}
```

**Error Responses**:
- **400 Bad Request**: Missing required fields
  ```json
  {"error": "title is required"}
  ```
- **404 Not Found**: Finding not found
  ```json
  {"error": "finding with id 789 not found"}
  ```

### GET /api/v1/work-orders

List all work orders with optional filters.

**Query Parameters**:
- `status`: Filter by status (`pending`, `in_progress`, `approved`, `rejected`, `completed`)
- `severity`: Filter by severity (`critical`, `high`, `medium`, `low`)
- `priority`: Filter by priority (`P1`, `P2`, `P3`, `P4`)
- `page`: Page number for pagination (default: 1)
- `per_page`: Items per page (default: 20, max: 100)

**Success Response** (200 OK):
```json
{
  "total": 45,
  "page": 1,
  "per_page": 20,
  "items": [
    {
      "id": 1001,
      "title": "Patch Critical SQL Injection Vulnerability",
      "status": "pending",
      "severity": "critical",
      "priority": "P1",
      "finding_id": 789,
      "assigned_team": "backend-dev",
      "deadline": "2024-09-10T23:59:59Z",
      "created_at": "2024-09-05T14:30:00Z"
    },
    {
      "id": 1002,
      "title": "Update SSL Certificate Configuration",
      "status": "in_progress",
      "severity": "high",
      "priority": "P2",
      "finding_id": 790,
      "assigned_team": "infrastructure",
      "deadline": "2024-09-12T23:59:59Z",
      "created_at": "2024-09-05T12:15:00Z"
    }
  ]
}
```

### GET /api/v1/work-orders/:id

Get detailed information about a specific work order.

**URL Parameters**:
- `id`: Work order ID

**Success Response** (200 OK):
```json
{
  "id": 1001,
  "title": "Patch Critical SQL Injection Vulnerability",
  "description": "Update application to fix SQL injection found in login form at /api/v1/login endpoint",
  "status": "pending",
  "severity": "critical",
  "priority": "P1",
  "finding": {
    "id": 789,
    "title": "Critical SQL Injection Detected",
    "description": "Parameter 'username' vulnerable to SQL injection",
    "cvss_score": 9.8,
    "cwe_id": "CWE-89",
    "mitre_technique": "T1566.002",
    "evidence": "SELECT * FROM users WHERE username='admin' OR '1'='1'",
    "remediation_guidance": "Use parameterized queries or prepared statements"
  },
  "assigned_team": "backend-dev",
  "assigned_to": null,
  "deadline": "2024-09-10T23:59:59Z",
  "estimated_hours": 8,
  "actual_hours": null,
  "tags": ["security", "sql-injection", "p1-priority"],
  "created_by": "admin",
  "created_at": "2024-09-05T14:30:00Z",
  "updated_at": "2024-09-05T14:30:00Z"
}
```

### PUT /api/v1/work-orders/:id

Update work order details.

**URL Parameters**:
- `id`: Work order ID

**Request Body**:
```json
{
  "title": "URGENT: Patch Critical SQL Injection",
  "description": "Fix now - production exploit attempts detected",
  "status": "in_progress",
  "assigned_to": "john.doe@company.com",
  "priority": "P0"
}
```

**Success Response** (200 OK):
```json
{
  "id": 1001,
  "title": "URGENT: Patch Critical SQL Injection",
  "status": "in_progress",
  "updated_at": "2024-09-05T15:00:00Z"
}
```

### DELETE /api/v1/work-orders/:id

Delete a work order (requires admin privileges).

**URL Parameters**:
- `id`: Work order ID

**Success Response** (200 OK):
```json
{
  "message": "Work order deleted successfully"
}
```

### POST /api/v1/work-orders/:id/approve

Approve work order for implementation.

**URL Parameters**:
- `id`: Work order ID

**Success Response** (200 OK):
```json
{
  "id": 1001,
  "status": "approved",
  "approved_by": "security-manager",
  "approved_at": "2024-09-05T15:30:00Z",
  "message": "Work order approved - development team can begin implementation"
}
```

### POST /api/v1/work-orders/:id/reject

Reject work order.

**URL Parameters**:
- `id`: Work order ID

**Request Body**:
```json
{
  "reason": "False positive - input validation already in place"
}
```

**Success Response** (200 OK):
```json
{
  "id": 1001,
  "status": "rejected",
  "rejected_by": "security-analyst",
  "rejected_at": "2024-09-05T15:45:00Z",
  "reason": "False positive - input validation already in place"
}
```

### GET /api/v1/work-orders/:id/audit

Get audit trail for work order lifecycle.

**URL Parameters**:
- `id`: Work order ID

**Success Response** (200 OK):
```json
{
  "work_order_id": 1001,
  "audit_trail": [
    {
      "timestamp": "2024-09-05T14:30:00Z",
      "action": "created",
      "performed_by": "scanner-bot",
      "details": "Work order created from finding #789",
      "metadata": {
        "finding_id": 789,
        "original_title": "Critical SQL Injection"
      }
    },
    {
      "timestamp": "2024-09-05T14:35:00Z",
      "action": "comment_added",
      "performed_by": "security-analyst",
      "details": "Added comment: Priority escalated due to production exploit attempts"
    },
    {
      "timestamp": "2024-09-05T15:30:00Z",
      "action": "approved",
      "performed_by": "security-manager",
      "details": "Approved for immediate implementation"
    }
  ]
}
```

---

## Attack Campaigns

Attack campaigns orchestrate multi-step red team engagements.

### POST /api/v1/campaigns

Create a new attack campaign.

**Request Body**:
```json
{
  "name": "Enterprise AD Penetration Test Q3 2024",
  "description": "Comprehensive Active Directory security assessment including kerberoasting, lateral movement, and privilege escalation testing",
  "targets": [
    {
      "domain": "corp.example.com",
      "type": "active_directory",
      "authorization_id": "AUTH-2024-Q3-AD-001"
    },
    {
      "url": "https://app.example.com",
      "type": "web_application",
      "authorization_id": "AUTH-2024-Q3-WEB-001"
    }
  ],
  "objectives": [
    "Compromise domain controller",
    "Extract sensitive data from CRM system",
    "Demonstrate business impact of identified vulnerabilities"
  ],
  "methodologies": [
    "MITRE ATT&CK Framework",
    "PTES (Penetration Testing Execution Standard)",
    "NIST SP 800-115"
  ],
  "start_date": "2024-09-10T09:00:00Z",
  "end_date": "2024-09-12T17:00:00Z",
  "constraints": {
    "max_parallel_attacks": 3,
    "avoid_service_disruption": true,
    "maintenance_window_only": false,
    "excluded_endpoints": [
      "/api/payment-processing",
      "/api/order-f fulfillment"
    ]
  },
  "notify_on_critical": true,
  "notify_email": "security-team@example.com"
}
```

**Success Response** (201 Created):
```json
{
  "id": 501,
  "name": "Enterprise AD Penetration Test Q3 2024",
  "status": "created",
  "progress_percentage": 0,
  "targets_count": 2,
  "start_date": "2024-09-10T09:00:00Z",
  "end_date": "2024-09-12T17:00:00Z",
  "created_at": "2024-09-05T16:00:00Z",
  "updated_at": "2024-09-05T16:00:00Z"
}
```

### GET /api/v1/campaigns

List all attack campaigns.

**Query Parameters**:
- `status`: Filter by status (`created`, `planning`, `running`, `paused`, `completed`, `cancelled`)
- `target_domain`: Filter by target domain
- `page`: Page number (default: 1)
- `per_page`: Items per page (default: 20)

**Success Response** (200 OK):
```json
{
  "total": 15,
  "page": 1,
  "per_page": 20,
  "items": [
    {
      "id": 501,
      "name": "Enterprise AD Penetration Test Q3 2024",
      "status": "running",
      "progress_percentage": 45,
      "targets": [
        {"domain": "corp.example.com", "type": "active_directory"},
        {"url": "https://app.example.com", "type": "web_application"}
      ],
      "current_phase": "exploitation",
      "findings_found": 23,
      "started_at": "2024-09-10T09:15:00Z",
      "created_at": "2024-09-05T16:00:00Z"
    },
    {
      "id": 500,
      "name": "E-commerce Web App Security Assessment",
      "status": "completed",
      "progress_percentage": 100,
      "targets": [
        {"url": "https://shop.example.com", "type": "web_application"}
      ],
      "current_phase": "reporting",
      "findings_found": 42,
      "completed_at": "2024-09-08T14:30:00Z",
      "created_at": "2024-09-01T10:00:00Z"
    }
  ]
}
```

### GET /api/v1/campaigns/:id

Get detailed campaign information.

**URL Parameters**:
- `id`: Campaign ID

**Success Response** (200 OK):
```json
{
  "id": 501,
  "name": "Enterprise AD Penetration Test Q3 2024",
  "description": "Comprehensive Active Directory security assessment",
  "status": "running",
  "progress_percentage": 45,
  "targets": [
    {
      "domain": "corp.example.com",
      "type": "active_directory",
      "authorization_id": "AUTH-2024-Q3-AD-001",
      "status": "compromised",
      "last_scanned": "2024-09-10T14:30:00Z"
    },
    {
      "url": "https://app.example.com",
      "type": "web_application",
      "authorization_id": "AUTH-2024-Q3-WEB-001",
      "status": "scanning",
      "last_scanned": "2024-09-10T14:25:00Z"
    }
  ],
  "objectives": [
    "Compromise domain controller",
    "Extract sensitive data from CRM system",
    "Demonstrate business impact of identified vulnerabilities"
  ],
  "methodologies": ["MITRE ATT&CK Framework", "PTES", "NIST SP 800-115"],
  "phases": [
    {
      "name": "Reconnaissance",
      "status": "completed",
      "progress_percentage": 100,
      "started_at": "2024-09-10T09:00:00Z",
      "completed_at": "2024-09-10T11:30:00Z",
      "findings_count": 8,
      "techniques_used": [
        {"technique_id": "T1595.001", "name": "Active Scanning", "success_rate": 85},
        {"technique_id": "T1592.002", "name": "Gather Victim Host Information", "success_rate": 92}
      ]
    },
    {
      "name": "Exploitation",
      "status": "in_progress",
      "progress_percentage": 65,
      "started_at": "2024-09-10T11:30:00Z",
      "findings_count": 15,
      "techniques_used": [
        {"technique_id": "T1110.004", "name": "Brute Force: Password Spraying", "success_rate": 35},
        {"technique_id": "T1550.004", "name": "Pass Hash Attack", "success_rate": 78}
      ]
    },
    {
      "name": "Post-Exploitation",
      "status": "pending",
      "progress_percentage": 0
    },
    {
      "name": "Reporting",
      "status": "pending",
      "progress_percentage": 0
    }
  ],
  "statistics": {
    "total_attempts": 234,
    "successful_exploits": 45,
    "detection_rate": 12.5,
    "evasion_rate": 87.5,
    "time_elapsed": "5h 30m",
    "time_remaining_estimated": "3h 20m"
  },
  "notified_contacts": ["security-team@example.com"],
  "created_by": "redteam-lead",
  "created_at": "2024-09-05T16:00:00Z",
  "started_at": "2024-09-10T09:15:00Z",
  "updated_at": "2024-09-10T14:30:00Z"
}
```

### POST /api/v1/campaigns/:id/start

Start a campaign (transitions from `created` → `running`).

**URL Parameters**:
- `id`: Campaign ID

**Success Response** (200 OK):
```json
{
  "id": 501,
  "name": "Enterprise AD Penetration Test Q3 2024",
  "previous_status": "created",
  "current_status": "running",
  "started_at": "2024-09-10T09:15:00Z",
  "message": "Campaign started successfully - execution engine initialized"
}
```

### POST /api/v1/campaigns/:id/pause

Pause an ongoing campaign.

**URL Parameters**:
- `id`: Campaign ID

**Success Response** (200 OK):
```json
{
  "id": 501,
  "previous_status": "running",
  "current_status": "paused",
  "paused_at": "2024-09-10T15:00:00Z",
  "paused_by": "security-manager",
  "pause_reason": "Emergency maintenance window requested",
  "message": "Campaign paused - state saved, resources preserved"
}
```

### POST /api/v1/campaigns/:id/cancel

Cancel a campaign permanently.

**URL Parameters**:
- `id`: Campaign ID

**Request Body**:
```json
{
  "reason": "Authorization expired - need to renew legal approval"
}
```

**Success Response** (200 OK):
```json
{
  "id": 501,
  "previous_status": "running",
  "current_status": "cancelled",
  "cancelled_at": "2024-09-10T15:30:00Z",
  "cancelled_by": "security-manager",
  "final_statistics": {
    "duration": "6h 15m",
    "phases_completed": 2,
    "findings_discovered": 23,
    "exploits_successful": 12
  },
  "message": "Campaign cancelled - final report will be generated"
}
```

---

## Vulnerability Findings

Findings represent individual security vulnerabilities discovered during campaigns.

### POST /api/v1/findings

Create a new finding (typically automated from scan results).

**Request Body**:
```json
{
  "title": "Critical SQL Injection in User Login",
  "description": "The 'username' parameter in the /api/v1/login endpoint is vulnerable to SQL injection. An attacker can bypass authentication or extract database contents.",
  "severity": "critical",
  "confidence": 95,
  "cvss_score": 9.8,
  "cvss_vector": "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:C/C:H/I:H/A:H",
  "cwe_id": "CWE-89",
  "mitre_techniques": ["T1190", "T1566.002"],
  "affected_endpoints": [
    {
      "url": "https://app.example.com/api/v1/login",
      "method": "POST",
      "vulnerable_parameter": "username",
      "evidence": "Input 'admin' OR '1'='1' returned successful authentication"
    }
  ],
  "remediation": {
    "guidance": "Use parameterized queries or prepared statements. Implement input validation and output encoding.",
    "references": [
      "https://owasp.org/www-community/attacks/SQL_Injection",
      "https://cheatsheetseries.owasp.org/cheatsheets/SQL_Injection_Prevention_Cheat_Sheet.html"
    ],
    "code_example": "// Python - Use parameterized query\ncursor.execute(\n    \"SELECT * FROM users WHERE username = ? AND password = ?\",\n    (username, password)\n)"
  },
  "campaign_id": 501,
  "automatic": true,
  "evidence": {
    "request": "POST /api/v1/login\nContent-Type: application/json\n{\"username\": \"admin' OR '1'='1\", \"password\": \"test\"}",
    "response": "{\"success\": true, \"user\": {\"id\": 1, \"username\": \"admin\"}}",
    "screenshot_url": "/evidence/screenshot-12345.png",
    "packet_capture": "/evidence/packet-12345.pcap"
  }
}
```

**Success Response** (201 Created):
```json
{
  "id": 789,
  "title": "Critical SQL Injection in User Login",
  "status": "open",
  "severity": "critical",
  "confidence": 95,
  "cvss_score": 9.8,
  "created_at": "2024-09-10T14:30:00Z"
}
```

### GET /api/v1/findings

List all findings with filtering options.

**Query Parameters**:
- `status`: Filter by status (`open`, `acknowledged`, `in_progress`, `fixed`, `closed`)
- `severity`: Filter by severity (`critical`, `high`, `medium`, `low`)
- `campaign_id`: Filter by campaign
- `tag`: Filter by tag
- `sort_by`: Sort field (`created_at`, `cvss_score`, `severity`)
- `sort_order`: Sort order (`asc`, `desc`)
- `page`: Page number (default: 1)
- `per_page`: Items per page (default: 20)

**Success Response** (200 OK):
```json
{
  "total": 156,
  "page": 1,
  "per_page": 20,
  "items": [
    {
      "id": 789,
      "title": "Critical SQL Injection in User Login",
      "severity": "critical",
      "cvss_score": 9.8,
      "status": "open",
      "confidence": 95,
      "cwe_id": "CWE-89",
      "mitre_techniques": ["T1190", "T1566.002"],
      "campaign_id": 501,
      "created_at": "2024-09-10T14:30:00Z",
      "updated_at": "2024-09-10T14:30:00Z",
      "tags": ["owasp-top-10", "sql-injection", "auth-bypass"]
    },
    {
      "id": 788,
      "title": "Broken Access Control Allows Data Exposure",
      "severity": "high",
      "cvss_score": 8.1,
      "status": "acknowledged",
      "confidence": 88,
      "cwe_id": "CWE-639",
      "mitre_techniques": ["T1550.005"],
      "campaign_id": 501,
      "created_at": "2024-09-10T13:45:00Z",
      "updated_at": "2024-09-10T14:00:00Z",
      "tags": ["owasp-top-10", "access-control", "data-leak"]
    }
  ]
}
```

### GET /api/v1/findings/:id

Get detailed information about a specific finding.

**URL Parameters**:
- `id`: Finding ID

**Success Response** (200 OK):
```json
{
  "id": 789,
  "title": "Critical SQL Injection in User Login",
  "description": "The 'username' parameter in the /api/v1/login endpoint accepts arbitrary SQL commands without proper sanitization, allowing attackers to bypass authentication or access unauthorized data.",
  "severity": "critical",
  "confidence": 95,
  "cvss_version": "3.1",
  "cvss_score": 9.8,
  "cvss_vector": "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:C/C:H/I:H/A:H",
  "base_metrics": {
    "attack_vector": "Network (N)",
    "attack_complexity": "Low (L)",
    "privileges_required": "None (N)",
    "user_interaction": "None (N)",
    "scope": "Changed (C)",
    "confidentiality": "High (H)",
    "integrity": "High (H)",
    "availability": "High (H)"
  },
  "cwe": {
    "id": "CWE-89",
    "name": "SQL Injection",
    "description": "Improper neutralization of special elements used in an SQL command"
  },
  "mitre_techniques": [
    {
      "technique_id": "T1190",
      "name": "Use Exploitable Public-facing Application",
      "tactic": "Initial Access"
    },
    {
      "technique_id": "T1566.002",
      "name": "Spearphishing Link",
      "tactic": "Initial Access"
    }
  ],
  "affected_resources": [
    {
      "type": "endpoint",
      "identifier": "https://app.example.com/api/v1/login",
      "method": "POST",
      "vulnerable_parameter": "username",
      "data_classification": "PII",
      "business_impact": "Authentication bypass could lead to full account takeover"
    }
  ],
  "evidence": {
    "request": {
      "method": "POST",
      "url": "/api/v1/login",
      "headers": {
        "Content-Type": "application/json",
        "User-Agent": "CloudAI-Fusion-Scanner/1.0"
      },
      "body": "{\"username\":\"admin' OR '1'='1\",\"password\":\"x\"}"
    },
    "response": {
      "status_code": 200,
      "headers": {"Content-Type": "application/json"},
      "body": "{\"success\":true,\"user\":{\"id\":1,\"username\":\"admin\",\"role\":\"administrator\"}}"
    },
    "comparison": {
      "legitimate_request": "{\"username\":\"admin\",\"password\":\"correct_password\"}",
      "malicious_payload": "{\"username\":\"admin' OR '1'='1\",\"password\":\"x\"}",
      "different_outcomes": "Both requests returned successful authentication"
    },
    "attachments": [
      {
        "type": "screenshot",
        "url": "/evidence/screenshots/789-login-exploit.png",
        "description": "Successful auth bypass screenshot"
      },
      {
        "type": "network_capture",
        "url": "/evidence/packets/789-request.pcap",
        "description": "Raw HTTP request/response capture"
      }
    ]
  },
  "remediation": {
    "priority": "immediate",
    "estimated_effort": "4-8 hours",
    "guidance": "Implement parameterized queries or prepared statements. All user inputs must be treated as untrusted data.",
    "verification_steps": [
      "Attempt original exploit payload - should fail or be sanitized",
      "Review code changes for all user input parameters",
      "Run DAST scan to verify no other injection points exist"
    ],
    "references": [
      {
        "title": "OWASP SQL Injection Prevention Cheat Sheet",
        "url": "https://cheatsheetseries.owasp.org/cheatsheets/SQL_Injection_Prevention_Cheat_Sheet.html"
      },
      {
        "title": "CWE-89: SQL Injection",
        "url": "https://cwe.mitre.org/data/definitions/89.html"
      }
    ],
    "code_examples": {
      "language": "python",
      "vulnerable": "cursor.execute(f\"SELECT * FROM users WHERE username='{username}'\")",
      "secure": "cursor.execute(\"SELECT * FROM users WHERE username=?\", (username,))"
    }
  },
  "detection_info": {
    "detection_method": "Automated DAST Scan + Manual Verification",
    "tool_used": "CloudAI Fusion OWASP ZAP Integration",
    "scan_profile": "Deep SQL Injection Detection",
    "detection_timestamp": "2024-09-10T14:30:00Z"
  },
  "lifecycle": {
    "status": "open",
    "status_history": [
      {"status": "open", "timestamp": "2024-09-10T14:30:00Z", "changed_by": "scanner-bot"}
    ],
    "workflow_stage": "triage",
    "sla_deadline": "2024-09-11T14:30:00Z",
    "escalation_level": 0
  },
  "campaign_id": 501,
  "related_findings": [788, 790],
  "tags": ["owasp-top-10", "sql-injection", "auth-bypass", "cwe-89"],
  "automatic": true,
  "created_by": "scanner-bot",
  "created_at": "2024-09-10T14:30:00Z",
  "updated_at": "2024-09-10T14:30:00Z"
}
```

### PUT /api/v1/findings/:id

Update finding attributes.

**URL Parameters**:
- `id`: Finding ID

**Request Body**:
```json
{
  "status": "in_progress",
  "severity": "critical",
  "assignee": "security-team@company.com",
  "notes": "Prioritized for immediate remediation - production traffic affected"
}
```

**Success Response** (200 OK):
```json
{
  "id": 789,
  "status": "in_progress",
  "updated_at": "2024-09-10T15:00:00Z"
}
```

### DELETE /api/v1/findings/:id

Delete a finding (requires admin privileges, typically for false positives).

**URL Parameters**:
- `id`: Finding ID

**Success Response** (200 OK):
```json
{
  "message": "Finding deleted successfully"
}
```

### POST /api/v1/findings/batch-update

Update multiple findings simultaneously.

**Request Body**:
```json
{
  "finding_ids": [789, 790, 791],
  "updates": {
    "status": "fixed",
    "notes": "All critical vulnerabilities have been patched and verified"
  }
}
```

**Success Response** (200 OK):
```json
{
  "updated_count": 3,
  "failed_updates": [],
  "message": "Successfully updated 3 findings"
}
```

### GET /api/v1/findings/export

Export findings report.

**Query Parameters**:
- `format`: Output format (`csv`, `json`, `pdf`, `html`, `markdown`)
- `campaign_id`: Export only findings from specific campaign
- `severity_filter`: Comma-separated severity levels
- `include_evidence`: Include evidence attachments (`true`, `false`)

**Success Response** (200 OK):
Content-Type depends on format parameter. For CSV:

```csv
ID,Title,Severity,CVSS Status,Confidence,CWE MITRE,Tech,Status,Created By,Created At
789,Critical SQL Injection in User Login,critical,9.8,open,95,CWE-89,T1190,T1566.002,open,scanner-bot,2024-09-10T14:30:00Z
788,Broken Access Control Allows Data Exposure,high,8.1,acknowledged,88,CWE-639,T1550.005,open,scanner-bot,2024-09-10T13:45:00Z
```

---

## Error Handling

All API errors follow a consistent format:

**Standard Error Response**:
```json
{
  "error": "error_code",
  "message": "Human-readable error message",
  "details": {}, // Optional additional context
  "request_id": "req_abc123xyz"
}
```

### Common Error Codes

| Error Code | HTTP Status | Description |
|------------|-------------|-------------|
| `invalid_credentials` | 401 | Username/password incorrect |
| `invalid_token` | 401 | JWT token invalid or expired |
| `missing_auth_header` | 401 | Authorization header missing |
| `unauthorized_action` | 403 | Insufficient permissions |
| `resource_not_found` | 404 | Resource doesn't exist |
| `validation_error` | 400 | Request body validation failed |
| `duplicate_resource` | 409 | Resource already exists |
| `internal_error` | 500 | Server-side error |
| `rate_limit_exceeded` | 429 | Too many requests |

**Example Error Response**:
```json
{
  "error": "validation_error",
  "message": "Request validation failed",
  "details": {
    "field_errors": [
      {"field": "email", "message": "Invalid email format"},
      {"field": "priority", "message": "Must be P0, P1, P2, P3, or P4"}
    ]
  },
  "request_id": "req_7f8a9b0c1d2e3f4g"
}
```

---

## SDK Examples

### Python SDK Example

```python
import requests

# Configuration
BASE_URL = "http://localhost:8080/api/v1"
API_KEY = "your-api-key"

# Helper function for authenticated requests
def api_request(method, endpoint, **kwargs):
    url = f"{BASE_URL}{endpoint}"
    headers = {
        "Authorization": f"Bearer {API_KEY}",
        "Content-Type": "application/json"
    }
    response = getattr(requests, method)(url, headers=headers, **kwargs)
    response.raise_for_status()
    return response.json()

# Example 1: Login and get token
login_response = requests.post(
    f"{BASE_URL}/auth/login",
    json={"username": "admin", "password": "strong-password"}
)
tokens = login_response.json()
api_key = tokens["access_token"]

# Example 2: Get dashboard statistics
stats = api_request("get", "/stats/dashboard")
print(f"Total Findings: {stats['total_findings']}")
print(f"Critical Issues: {stats['critical_findings']}")

# Example 3: Create attack campaign
campaign = {
    "name": "Test Campaign",
    "description": "Testing API integration",
    "targets": [
        {"domain": "example.com", "type": "web_application"}
    ],
    "start_date": "2024-09-15T09:00:00Z",
    "end_date": "2024-09-15T17:00:00Z"
}
new_campaign = api_request("post", "/campaigns", json=campaign)
print(f"Campaign created with ID: {new_campaign['id']}")

# Example 4: List findings
findings = api_request("get", "/findings?severity=critical")
for finding in findings["items"]:
    print(f"- {finding['title']} (CVSS: {finding['cvss_score']})")
```

### JavaScript/TypeScript SDK Example

```typescript
import axios from 'axios';

const BASE_URL = 'http://localhost:8080/api/v1';

class RedTeamClient {
  private accessToken: string;
  
  constructor(username: string, password: string) {
    this.login(username, password);
  }
  
  async login(username: string, password: string): Promise<void> {
    const response = await axios.post(`${BASE_URL}/auth/login`, {
      username,
      password
    });
    this.accessToken = response.data.access_token;
  }
  
  private getAuthHeaders(): Record<string, string> {
    return {
      'Authorization': `Bearer ${this.accessToken}`,
      'Content-Type': 'application/json'
    };
  }
  
  async getCampaign(id: string): Promise<any> {
    const response = await axios.get(
      `${BASE_URL}/campaigns/${id}`,
      { headers: this.getAuthHeaders() }
    );
    return response.data;
  }
  
  async createCampaign(campaignData: any): Promise<any> {
    const response = await axios.post(
      `${BASE_URL}/campaigns`,
      campaignData,
      { headers: this.getAuthHeaders() }
    );
    return response.data;
  }
  
  async listFindings(filters?: { severity?: string; status?: string }): Promise<any> {
    const params = new URLSearchParams(filters).toString();
    const response = await axios.get(
      `${BASE_URL}/findings?${params}`,
      { headers: this.getAuthHeaders() }
    );
    return response.data;
  }
}

// Usage
async function main() {
  const client = new RedTeamClient('admin', 'password');
  
  const campaign = await client.createCampaign({
    name: 'Security Assessment',
    targets: [{ domain: 'example.com', type: 'web_application' }]
  });
  
  console.log(`Campaign created: ${campaign.id}`);
  
  const findings = await client.listFindings({ severity: 'critical' });
  console.log(`Critical findings: ${findings.total}`);
}
```

### Bash/cURL Examples

```bash
# Login and store token
TOKEN=$(curl -s -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"password"}' | \
  jq -r '.access_token')

# Get dashboard stats
curl -s -X GET http://localhost:8080/api/v1/stats/dashboard \
  -H "Authorization: Bearer $TOKEN" | jq .

# List campaigns
curl -s -X GET "http://localhost:8080/api/v1/campaigns?status=running" \
  -H "Authorization: Bearer $TOKEN" | jq '.items[] | {id, name, progress}'

# Create finding manually
curl -s -X POST http://localhost:8080/api/v1/findings \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "title": "Manual finding test",
    "severity": "high",
    "description": "Manually created for testing"
  }' | jq .
```

---

## Rate Limiting & Throttling

**Rate Limits** (per IP address):
- Auth endpoints: 5 requests/minute
- General API: 100 requests/minute
- Report exports: 10 requests/hour

**Headers** included in all responses:
```
X-RateLimit-Limit: 100
X-RateLimit-Remaining: 95
X-RateLimit-Reset: 1694123456
```

When rate limited, receive:
```json
{
  "error": "rate_limit_exceeded",
  "message": "Too many requests. Please retry after 60 seconds.",
  "retry_after": 60
}
```

---

## Versioning

Current API version: **v1**

Breaking changes will increment the major version (v2, v3, etc.). Old versions remain supported for 12 months.

**Deprecation Notice**: Endpoints will be deprecated with 6-month advance notice via:
- HTTP `Deprecation` header
- API response warning messages
- Email notifications to registered users

---

*For more examples and advanced usage, see [USER_MANUAL.md](USER_MANUAL.md)*  
*Developer Guide available in [DEVELOPER_GUIDE.md](DEVELOPER_GUIDE.md)*
