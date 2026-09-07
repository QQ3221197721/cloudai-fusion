// API Client Library for CloudAI Fusion Red Team Platform
// Connects to real backend API server on http://localhost:8080/api/v1
const BASE_URL = import.meta.env.VITE_API_URL || "http://localhost:8080/api/v1";

interface LoginCredentials {
  username: string;
  password: string;
}

export interface AuthResponse {
  access_token: string;
  token_type: string;
  expires_in: number;
  user_id: string;
  username: string;
  role: string;
  permissions: string[];
  full_name?: string;
}

export interface User {
  id: string;
  username: string;
  email: string;
  role: string;
}

export interface Engagement {
  id: string;
  tenant_id?: string;
  scope: Scope;
  status: "pending" | "active" | "completed" | "aborted" | "approved";
  created_at: string;
  updated_at: string;
  findings?: Finding[];
}

export interface Scope {
  targets: string[];
  rules: Rule[];
  authorized_actions: string[];
}

export interface Rule {
  type: string;
  pattern: string;
}

export interface Finding {
  id: string;
  title: string;
  severity: "critical" | "high" | "medium" | "low" | "info";
  description: string;
  evidence: string[];
  remediation: string;
}

export interface DashboardStats {
  totalScans: number;
  activeCampaigns: number;
  threatsFound: number;
  pendingOrders: number;
  completedScans: number;
}

export class ApiClient {
  private token: string | null = null;

  constructor() {
    // Load token from localStorage on init
    const storedToken = localStorage.getItem('access_token');
    if (storedToken) {
      this.setToken(storedToken);
    }
  }

  setToken(token: string) {
    this.token = token;
    localStorage.setItem('access_token', token);
  }

  clearToken() {
    this.token = null;
    localStorage.removeItem("auth_token");
  }

  async login(credentials: LoginCredentials): Promise<AuthResponse> {
    try {
      const response = await fetch(`${BASE_URL}/auth/login`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
        },
        body: JSON.stringify(credentials),
      });

      if (!response.ok) {
        const error = await response.json();
        throw new Error(error.error || error.message || "Login failed");
      }

      const data: AuthResponse = await response.json();
      
      // Store token in localStorage
      if (data.access_token) {
        this.setToken(data.access_token);
        localStorage.setItem('user_id', data.user_id);
        localStorage.setItem('role', data.role);
        localStorage.setItem('username', data.username);
      }
      
      return data;
    } catch (error) {
      console.error('Login error:', error);
      throw error;
    }
  }

  async logout() {
    this.clearToken();
  }

  // Red Team Engagements
  async createEngagement(scope: Scope, tenantId?: string): Promise<Engagement> {
    const response = await fetch(`${BASE_URL}/redteam/engagements`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "Authorization": `Bearer ${this.token}`,
      },
      body: JSON.stringify({ scope, tenant_id: tenantId }),
    });

    if (!response.ok) {
      const error = await response.json();
      throw new Error(error.message || "Failed to create engagement");
    }

    return response.json();
  }

  async getEngagements(tenantId?: string): Promise<{ engagements: Engagement[]; total: number }> {
    const url = tenantId
      ? `${BASE_URL}/redteam/engagements?tenant_id=${tenantId}`
      : `${BASE_URL}/redteam/engagements`;

    const response = await fetch(url, {
      headers: {
        "Authorization": `Bearer ${this.token}`,
      },
    });

    if (!response.ok) {
      throw new Error("Failed to fetch engagements");
    }

    return response.json();
  }

  async getEngagement(id: string): Promise<Engagement> {
    const response = await fetch(`${BASE_URL}/redteam/engagements/${id}`, {
      headers: {
        "Authorization": `Bearer ${this.token}`,
      },
    });

    if (!response.ok) {
      throw new Error(`Engagement ${id} not found`);
    }

    return response.json();
  }

  async abortEngagement(id: string, reason?: string): Promise<void> {
    const response = await fetch(`${BASE_URL}/redteam/engagements/${id}/abort`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "Authorization": `Bearer ${this.token}`,
      },
      body: JSON.stringify({ reason: reason || "Aborted via API" }),
    });

    if (!response.ok) {
      throw new Error(`Failed to abort engagement ${id}`);
    }
  }

  async approveAction(engagementId: string, actionId: string, riskTier: number): Promise<void> {
    const response = await fetch(`${BASE_URL}/redteam/engagements/${engagementId}/approve`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "Authorization": `Bearer ${this.token}`,
      },
      body: JSON.stringify({ action_id: actionId, risk_tier: riskTier }),
    });

    if (!response.ok) {
      throw new Error(`Failed to approve action ${actionId}`);
    }
  }

  // Reports & Evidence
  async getReport(engagementId: string) {
    const response = await fetch(`${BASE_URL}/redteam/engagements/${engagementId}/report`, {
      headers: {
        "Authorization": `Bearer ${this.token}`,
      },
    });

    if (!response.ok) {
      throw new Error(`Failed to fetch report for ${engagementId}`);
    }

    return response.json();
  }

  async getEvidence(engagementId: string) {
    const response = await fetch(`${BASE_URL}/redteam/engagements/${engagementId}/evidence`, {
      headers: {
        "Authorization": `Bearer ${this.token}`,
      },
    });

    if (!response.ok) {
      throw new Error(`Failed to fetch evidence for ${engagementId}`);
    }

    return response.json();
  }

  // Dashboard Stats - Returns REAL live data from PostgreSQL
  async getDashboardStats(): Promise<{
    totalScans: number;
    activeCampaigns: number;
    threatsFound: number;
    pendingOrders: number;
    completedScans: number;
  }> {
    const token = this.getToken();
    if (!token) {
      throw new Error('Not authenticated');
    }

    const response = await fetch(`${BASE_URL}/dashboard`, {
      headers: {
        "Authorization": `Bearer ${token}`,
      },
    });

    if (!response.ok) {
      const error = await response.json();
      throw new Error(error.message || "Failed to fetch dashboard stats");
    }

    const data = await response.json();
    
    // Map backend response to frontend interface
    return {
      totalScans: data.total_scans || 0,
      activeCampaigns: data.active_campaigns || 0,
      threatsFound: data.threats_found || 0,
      pendingOrders: data.pending_orders || 0,
      completedScans: data.completed_scans || 0,
    };
  }

  // Quick Scan
  async quickScan(targets: string[]): Promise<{ scanId: string; status: string }> {
    const response = await fetch(`${BASE_URL}/quickscan`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "Authorization": `Bearer ${this.token}`,
      },
      body: JSON.stringify({ targets }),
    });

    if (!response.ok) {
      throw new Error("Quick scan failed");
    }

    return response.json();
  }

  // Work Order Submission - Submits to real database
  async submitWorkOrder(orderData: {
    companyName: string;
    email: string;
    justification: string;
    targets: string;
  }): Promise<{ orderId: string; status: string }> {
    const token = this.getToken();
    if (!token) {
      throw new Error('Not authenticated');
    }

    const response = await fetch(`${BASE_URL}/redteam/work-orders`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "Authorization": `Bearer ${token}`,
      },
      body: JSON.stringify(orderData),
    });

    if (!response.ok) {
      const error = await response.json();
      throw new Error(error.message || "Failed to submit work order");
    }

    return response.json();
  }

  // Get Work Orders - Fetches from real database
  async getWorkOrders(): Promise<any[]> {
    const token = this.getToken();
    if (!token) {
      throw new Error('Not authenticated');
    }

    const response = await fetch(`${BASE_URL}/redteam/work-orders`, {
      headers: {
        "Authorization": `Bearer ${token}`,
      },
    });

    if (!response.ok) {
      throw new Error("Failed to fetch work orders");
    }

    return response.json();
  }

  private getToken(): string | null {
    return this.token;
  }
}

export const apiClient = new ApiClient();
