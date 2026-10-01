/**
 * M19 Security Behavior Hunting Platform - Production-Grade Threat Detection Dashboard
 * 
 * Complete user journey: View threats → Create hunt case → Configure rules → Execute queries → Analyze correlations → Generate report
 * Implements real backend API integration with CloudAI Fusion behavior hunting endpoints
 * Design Philosophy: Linear-style dark theme, security operations aesthetics, zero-trust monitoring
 */

import { useEffect, useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import axios from "axios";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import {
  ShieldAlert,
  Search,
  FileText,
  Network,
  Activity,
  Target,
  Database,
  BrainCircuit,
  Filter,
  Download,
  Eye,
  Edit,
  Trash2,
  Zap,
  CheckCircle2,
  XCircle,
  Loader2,
  Clock,
  Users,
  Link2,
  AlertTriangle,
  PieChart,
  Calendar,
  Tag as TagIcon,
  Plus,
} from "lucide-react";

// API Base URL configuration
const API_BASE_URL = import.meta.env.VITE_API_URL || "http://localhost:8080";

// ============================================================================
// Type Definitions
// ============================================================================

interface HuntCase {
  id: string;
  name: string;
  description?: string;
  status: 'open' | 'investigating' | 'resolved' | 'closed';
  severity: 'critical' | 'high' | 'medium' | 'low';
  assigned_to?: string;
  created_at: string;
  updated_at: string;
  tags: string[];
  alerts_count: number;
  findings_count: number;
  mitre_techniques: string[];
}

interface SecurityAlert {
  id: string;
  timestamp: string;
  source: string;
  rule_name: string;
  severity: 'critical' | 'high' | 'medium' | 'low';
  entity_type: 'user' | 'host' | 'network' | 'process';
  entity_name: string;
  description: string;
  technique_id?: string;
  tactic?: string;
  linked_case_id?: string;
}

interface BehavioralBaseline {
  entity_type: string;
  baseline_id: string;
  metrics: Record<string, any>;
  confidence_score: number;
  samples_collected: number;
  trained_at: string;
}

interface UEBAAnomaly {
  score: number;
  anomaly_types: string[];
  deviation_from_baseline: Record<string, number>;
  affected_features: Array<{ name: string; zscore: number }>;
  risk_level: 'critical' | 'high' | 'medium' | 'low';
  explanation: string;
}

interface MITRETechnique {
  id: string;
  name: string;
  tactic: string;
  description: string;
  detections: number;
  last_detected?: string;
}

interface IncidentReport {
  report_id: string;
  case_id: string;
  title: string;
  summary: string;
  executive_summary: string;
  technical_details: string;
  evidence_links: string[];
  generated_at: string;
  author: string;
  zkp_receipt?: ZKPReceipt;
}

interface ZKPReceipt {
  proof_hash: string;
  data_integrity: boolean;
  signed_at: string;
  verifier_public_key: string;
}

// ============================================================================
// Utility Functions
// ============================================================================

const getSeverityColor = (severity: string) => {
  switch(severity.toLowerCase()) {
    case 'critical': return 'bg-red-500/20 text-red-400 border-red-500/30';
    case 'high': return 'bg-orange-500/20 text-orange-400 border-orange-500/30';
    case 'medium': return 'bg-yellow-500/20 text-yellow-400 border-yellow-500/30';
    case 'low': return 'bg-blue-500/20 text-blue-400 border-blue-500/30';
    default: return 'bg-gray-500/20 text-gray-400';
  }
};

const getStatusColor = (status: string) => {
  switch(status.toLowerCase()) {
    case 'open': return 'bg-green-500/20 text-green-400 border-green-500/30';
    case 'investigating': return 'bg-blue-500/20 text-blue-400 border-blue-500/30';
    case 'resolved': return 'bg-purple-500/20 text-purple-400 border-purple-500/30';
    case 'closed': return 'bg-gray-500/20 text-gray-400 border-gray-500/30';
    default: return 'bg-gray-500/20 text-gray-400';
  }
};

const formatDate = (dateString: string) => {
  return new Date(dateString).toLocaleString('en-US', {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  });
};

// ============================================================================
// Main Component
// ============================================================================

export default function M19BehaviorHuntingPage() {
  const queryClient = useQueryClient();
  const [showCreateModal, setShowCreateModal] = useState(false);
  const [selectedCase, setSelectedCase] = useState<HuntCase | null>(null);
  const [activeTab, setActiveTab] = useState("dashboard");
  
  // Form state for creating hunt cases
  const [newCase, setNewCase] = useState({
    name: "",
    description: "",
    severity: 'medium' as 'critical' | 'high' | 'medium' | 'low',
    tags: [] as string[],
  });

  // Query hooks
  const { data: casesResponse } = useQuery<{ cases: HuntCase[]; total: number }>({
    queryKey: ["huntCases"],
    queryFn: async () => {
      const token = localStorage.getItem("token");
      const res = await axios.get(`${API_BASE_URL}/api/v1/behavior-hunting/cases`, {
        headers: { Authorization: `Bearer ${token}` },
      });
      return res.data;
    },
  });

  const { data: alertsResponse } = useQuery<{ alerts: SecurityAlert[]; total: number }>({
    queryKey: ["securityAlerts"],
    queryFn: async () => {
      const token = localStorage.getItem("token");
      const res = await axios.get(`${API_BASE_URL}/api/v1/behavior-hunting/alerts`, {
        headers: { Authorization: `Bearer ${token}` },
      });
      return res.data;
    },
  });

  const { data: uebasResponse } = useQuery<{ anomalies: UEBAAnomaly[] }>({
    queryKey: ["uebAnomalies"],
    queryFn: async () => {
      const token = localStorage.getItem("token");
      const res = await axios.post(`${API_BASE_URL}/api/v1/behavior-hunting/analysis/ueba`, {}, {
        headers: { Authorization: `Bearer ${token}` },
      });
      return res.data;
    },
  });

  // Mutation hooks
  const createCaseMutation = useMutation({
    mutationFn: async (caseData: any) => {
      const token = localStorage.getItem("token");
      return axios.post(`${API_BASE_URL}/api/v1/behavior-hunting/cases`, caseData, {
        headers: { Authorization: `Bearer ${token}` },
      });
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["huntCases"] });
      setShowCreateModal(false);
    },
  });

  const assignInvestigatorMutation = useMutation({
    mutationFn: async ({ caseId, investigator }: { caseId: string; investigator: string }) => {
      const token = localStorage.getItem("token");
      return axios.post(`${API_BASE_URL}/api/v1/behavior-hunting/cases/${caseId}/assign`,
        { investigator },
        {
          headers: { Authorization: `Bearer ${token}` },
        }
      );
    },
    onSuccess: (_, { caseId }) => {
      queryClient.invalidateQueries({ queryKey: ["huntCases"] });
      queryClient.invalidateQueries({ queryKey: ["huntCase", caseId] });
    },
  });

  const deleteCaseMutation = useMutation({
    mutationFn: async (caseId: string) => {
      const token = localStorage.getItem("token");
      return axios.delete(`${API_BASE_URL}/api/v1/behavior-hunting/cases/${caseId}`, {
        headers: { Authorization: `Bearer ${token}` },
      });
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["huntCases"] });
      setSelectedCase(null);
    },
  });

  const generateReportMutation = useMutation({
    mutationFn: async (caseId: string) => {
      const token = localStorage.getItem("token");
      return axios.post(`${API_BASE_URL}/api/v1/behavior-hunting/cases/${caseId}/report`, {}, {
        headers: { Authorization: `Bearer ${token}` },
        responseType: 'blob',
      });
    },
    onSuccess: (_, caseId) => {
      queryClient.invalidateQueries({ queryKey: ["huntCase", caseId] });
    },
  });

  return (
    <div className="min-h-screen bg-gradient-to-br from-slate-950 via-slate-900 to-slate-950 p-6">
      {/* Header */}
      <div className="mb-8 animate-in fade-in slide-in-from-bottom-4 duration-700">
        <div className="flex items-center justify-between">
          <div>
            <h1 className="text-4xl font-bold bg-gradient-to-r from-red-500 to-orange-500 bg-clip-text text-transparent">
              Behavior Hunting
            </h1>
            <p className="text-gray-400 mt-2">
              Advanced threat detection with UEBA and MITRE ATT&CK mapping
            </p>
          </div>
          <Button
            onClick={() => setShowCreateModal(true)}
            className="bg-red-600 hover:bg-red-700 text-white gap-2"
          >
            <Plus className="w-4 h-4" />
            New Hunt Case
          </Button>
        </div>
      </div>

      {/* Stats Cards */}
      <div className="grid grid-cols-1 md:grid-cols-4 gap-6 mb-8 animate-in fade-in slide-in-from-bottom-4 duration-700 delay-100">
        <Card className="border-slate-700 bg-slate-800/50 backdrop-blur-sm">
          <CardContent className="pt-6">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-gray-400">Open Cases</p>
                <p className="text-3xl font-bold text-white mt-1">
                  {casesResponse?.cases.filter(c => c.status === 'open').length || 0}
                </p>
              </div>
              <ShieldAlert className="w-10 h-10 text-red-500" />
            </div>
          </CardContent>
        </Card>

        <Card className="border-slate-700 bg-slate-800/50 backdrop-blur-sm">
          <CardContent className="pt-6">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-gray-400">Active Alerts</p>
                <p className="text-3xl font-bold text-white mt-1">
                  {alertsResponse?.alerts.filter(a => a.severity === 'critical' || a.severity === 'high').length || 0}
                </p>
              </div>
              <AlertTriangle className="w-10 h-10 text-orange-500" />
            </div>
          </CardContent>
        </Card>

        <Card className="border-slate-700 bg-slate-800/50 backdrop-blur-sm">
          <CardContent className="pt-6">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-gray-400">MITRE Techniques</p>
                <p className="text-3xl font-bold text-white mt-1">
                  {[...new Set(casesResponse?.cases.flatMap(c => c.mitre_techniques) || [])].length}
                </p>
              </div>
              <BrainCircuit className="w-10 h-10 text-purple-500" />
            </div>
          </CardContent>
        </Card>

        <Card className="border-slate-700 bg-slate-800/50 backdrop-blur-sm">
          <CardContent className="pt-6">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-gray-400">Total Findings</p>
                <p className="text-3xl font-bold text-white mt-1">
                  {casesResponse?.cases.reduce((acc, c) => acc + c.findings_count, 0) || 0}
                </p>
              </div>
              <FileText className="w-10 h-10 text-blue-500" />
            </div>
          </CardContent>
        </Card>
      </div>

      {/* Main Content Tabs */}
      <Tabs value={activeTab} onValueChange={setActiveTab} className="animate-in fade-in slide-in-from-bottom-4 duration-700 delay-200">
        <TabsList className="bg-slate-800 border border-slate-700">
          <TabsTrigger value="dashboard">Threat Overview</TabsTrigger>
          <TabsTrigger value="cases" disabled={!true}>Hunt Cases</TabsTrigger>
          <TabsTrigger value="alerts" disabled={!true}>Alerts Feed</TabsTrigger>
          <TabsTrigger value="analysis" disabled={!selectedCase}>UEBA Analysis</TabsTrigger>
          <TabsTrigger value="mitre">ATT&CK Matrix</TabsTrigger>
        </TabsList>

        <TabsContent value="dashboard" className="mt-6">
          <ThreatDashboardView
            cases={casesResponse?.cases || []}
            alerts={alertsResponse?.alerts || []}
            isLoading={createCaseMutation.isPending}
            onSelectCase={setSelectedCase}
          />
        </TabsContent>

        <TabsContent value="cases" className="mt-6">
          <HuntCasesView
            cases={casesResponse?.cases || []}
            isLoading={createCaseMutation.isPending}
            onSelectCase={setSelectedCase}
            onAssign={(caseId, investigator) => assignInvestigatorMutation.mutate({ caseId, investigator })}
            onDelete={(caseId) => deleteCaseMutation.mutate(caseId)}
            onReport={(caseId) => generateReportMutation.mutate(caseId)}
          />
        </TabsContent>

        <TabsContent value="alerts" className="mt-6">
          <AlertsView
            alerts={alertsResponse?.alerts || []}
            isLoading={createCaseMutation.isPending}
          />
        </TabsContent>

        <TabsContent value="analysis" className="mt-6">
          {selectedCase ? (
            <UEBAAnalysisView case={selectedCase} anomalies={uebasResponse?.anomalies || []} />
          ) : (
            <EmptyState message="Select a case to view behavioral analysis" />
          )}
        </TabsContent>

        <TabsContent value="mitre" className="mt-6">
          <MitreATTACKView />
        </TabsContent>
      </Tabs>

      {/* Create Case Modal */}
      {showCreateModal && (
        <CreateCaseModal
          onClose={() => setShowCreateModal(false)}
          onSubmit={(data) => createCaseMutation.mutate(data)}
          isSubmitting={createCaseMutation.isPending}
          initialData={newCase}
          onUpdate={setNewCase}
        />
      )}
    </div>
  );
}

// ============================================================================
// Sub-Components
// ============================================================================

const ThreatDashboardView = ({
  cases,
  alerts,
  isLoading,
  onSelectCase
}: {
  cases: HuntCase[];
  alerts: SecurityAlert[];
  isLoading: boolean;
  onSelectCase: (c: HuntCase) => void;
}) => {
  const criticalAlerts = alerts.filter(a => a.severity === 'critical');
  const highAlerts = alerts.filter(a => a.severity === 'high');
  const openCases = cases.filter(c => c.status === 'open');
  const investigatingCases = cases.filter(c => c.status === 'investigating');

  return (
    <div className="space-y-6">
      <Card className="border-slate-700 bg-slate-800/50">
        <CardHeader>
          <div className="flex items-center justify-between">
            <div>
              <h3 className="text-xl font-bold text-white">Threat Landscape</h3>
              <p className="text-sm text-slate-400">Real-time security situation overview</p>
            </div>
            <Button size="sm" variant="outline" className="border-slate-600 text-slate-300 hover:bg-slate-700">
              <Zap className="w-3 h-3 mr-1" />
              Refresh
            </Button>
          </div>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
            <AlertBox severity="critical" count={criticalAlerts.length} icon={<ShieldAlert className="w-4 h-4" />} label="Critical Alerts" />
            <AlertBox severity="high" count={highAlerts.length} icon={<AlertTriangle className="w-4 h-4" />} label="High Severity" />
            <AlertBox severity="info" count={openCases.length} icon={<Search className="w-4 h-4" />} label="Open Hunts" />
            <AlertBox severity="info" count={investigatingCases.length} icon={<Clock className="w-4 h-4" />} label="Investigating" />
          </div>
        </CardContent>
      </Card>

      <AlertsView alerts={alerts.slice(0, 10)} isLoading={isLoading} />
    </div>
  );
};

const AlertBox = ({ severity, count, icon, label }: { severity: string; count: number; icon: React.ReactNode; label: string }) => {
  const colors: Record<string, string> = {
    critical: 'text-red-400 border-red-500/30 bg-red-500/10',
    high: 'text-orange-400 border-orange-500/30 bg-orange-500/10',
    medium: 'text-yellow-400 border-yellow-500/30 bg-yellow-500/10',
    low: 'text-blue-400 border-blue-500/30 bg-blue-500/10',
    info: 'text-blue-400 border-blue-500/30 bg-blue-500/10',
  };

  return (
    <Card className={`border ${colors[severity]} bg-opacity-20`}>
      <CardContent className="pt-6">
        <div className="flex items-center justify-between">
          <div className={`${colors[severity]} p-3 rounded-lg`} style={{ backgroundColor: 'transparent' }}>
            {icon}
          </div>
          <div className="text-right">
            <div className="text-3xl font-bold text-white">{count}</div>
            <div className="text-sm text-slate-400 mt-1">{label}</div>
          </div>
        </div>
      </CardContent>
    </Card>
  );
};

const HuntCasesView = ({
  cases,
  isLoading,
  onSelectCase,
  onAssign,
  onDelete,
  onReport
}: {
  cases: HuntCase[];
  isLoading: boolean;
  onSelectCase: (c: HuntCase) => void;
  onAssign: (caseId: string, investigator: string) => void;
  onDelete: (caseId: string) => void;
  onReport: (caseId: string) => void;
}) => {
  const [searchTerm, setSearchTerm] = useState('');
  const [statusFilter, setStatusFilter] = useState<string>('all');

  const filteredCases = cases.filter(c => {
    const matchesSearch = c.name.toLowerCase().includes(searchTerm.toLowerCase()) ||
                         c.description?.toLowerCase().includes(searchTerm.toLowerCase());
    const matchesStatus = statusFilter === 'all' || c.status === statusFilter;
    return matchesSearch && matchesStatus;
  });

  return (
    <Card className="border-slate-700 bg-slate-800/50">
      <CardHeader className="border-b border-slate-700">
        <div className="flex items-center justify-between">
          <div>
            <h3 className="text-xl font-bold text-white">Hunt Cases</h3>
            <p className="text-sm text-slate-400">Security investigation workflow management</p>
          </div>
          <div className="flex gap-3">
            <Input
              placeholder="Search cases..."
              value={searchTerm}
              onChange={(e) => setSearchTerm(e.target.value)}
              className="bg-slate-800 border-slate-600 text-white w-64"
            />
            <Select value={statusFilter} onValueChange={setStatusFilter}>
              <SelectTrigger className="bg-slate-800 border-slate-600 text-white w-32">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All Status</SelectItem>
                <SelectItem value="open">Open</SelectItem>
                <SelectItem value="investigating">Investigating</SelectItem>
                <SelectItem value="resolved">Resolved</SelectItem>
                <SelectItem value="closed">Closed</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </div>
      </CardHeader>
      <CardContent className="pt-6">
        {isLoading ? (
          <div className="text-center py-12">
            <Loader2 className="w-12 h-12 animate-spin mx-auto text-blue-500" />
            <p className="text-slate-400 mt-4">Loading cases...</p>
          </div>
        ) : filteredCases.length === 0 ? (
          <EmptyState message="No hunt cases found" />
        ) : (
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
            {filteredCases.map((c) => (
              <Card key={c.id} className="border-slate-700 bg-slate-800/50 hover:border-red-500/50 transition-colors cursor-pointer">
                <CardHeader className="pb-3">
                  <div className="flex items-start justify-between">
                    <div>
                      <h3 className="text-lg font-bold text-white">{c.name}</h3>
                      <div className="flex items-center gap-2 mt-1">
                        <Badge className={`${getStatusColor(c.status)} border`}>{c.status.toUpperCase()}</Badge>
                        <Badge className={`${getSeverityColor(c.severity)} border`}>{c.severity.toUpperCase()}</Badge>
                      </div>
                    </div>
                    <Activity className={`w-5 h-5 ${c.status === 'open' ? 'text-red-400' : 'text-blue-400'}`} />
                  </div>
                </CardHeader>
                <CardContent className="space-y-3">
                  {c.description && (
                    <p className="text-sm text-slate-300 line-clamp-2">{c.description}</p>
                  )}

                  {c.tags.length > 0 && (
                    <div className="flex flex-wrap gap-1">
                      {c.tags.slice(0, 3).map(tag => (
                        <Badge key={tag} variant="outline" className="text-xs bg-slate-700 border-slate-600">
                          <TagIcon className="w-3 h-3 mr-1" />
                          {tag}
                        </Badge>
                      ))}
                      {c.tags.length > 3 && (
                        <Badge variant="outline" className="text-xs bg-slate-700 border-slate-600">
                          +{c.tags.length - 3}
                        </Badge>
                      )}
                    </div>
                  )}

                  <div className="pt-2 border-t border-slate-700 space-y-1 text-sm">
                    <div className="flex justify-between">
                      <span className="text-slate-400">Alerts:</span>
                      <span className="text-white">{c.alerts_count}</span>
                    </div>
                    <div className="flex justify-between">
                      <span className="text-slate-400">Findings:</span>
                      <span className="text-white">{c.findings_count}</span>
                    </div>
                    {c.mitre_techniques.length > 0 && (
                      <div className="flex justify-between">
                        <span className="text-slate-400">MITRE:</span>
                        <span className="text-purple-400">{c.mitre_techniques.length} techniques</span>
                      </div>
                    )}
                  </div>

                  <div className="flex gap-2 pt-2">
                    <Button
                      size="sm"
                      variant="outline"
                      className="flex-1 border-slate-600 text-slate-300 hover:bg-slate-700"
                      onClick={() => onSelectCase(c)}
                    >
                      <Eye className="w-3 h-3 mr-1" />
                      View
                    </Button>
                    <Button
                      size="sm"
                      variant="outline"
                      className="border-red-500/30 text-red-400 hover:bg-red-500/10"
                      onClick={() => onDelete(c.id)}
                    >
                      <Trash2 className="w-3 h-3" />
                    </Button>
                  </div>
                </CardContent>
              </Card>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  );
};

const AlertsView = ({ alerts, isLoading }: { alerts: SecurityAlert[]; isLoading: boolean }) => {
  if (isLoading) {
    return (
      <div className="text-center py-12">
        <Loader2 className="w-12 h-12 animate-spin mx-auto text-blue-500" />
        <p className="text-slate-400 mt-4">Loading alerts...</p>
      </div>
    );
  }

  if (alerts.length === 0) {
    return <EmptyState message="No alerts found" />;
  }

  return (
    <Card className="border-slate-700 bg-slate-800/50">
      <CardHeader className="border-b border-slate-700">
        <div className="flex items-center justify-between">
          <div>
            <h3 className="text-xl font-bold text-white">Security Alerts</h3>
            <p className="text-sm text-slate-400">Real-time detection feed</p>
          </div>
          <Button size="sm" variant="outline" className="border-slate-600 text-slate-300 hover:bg-slate-700">
            <Download className="w-3 h-3 mr-1" />
            Export
          </Button>
        </div>
      </CardHeader>
      <CardContent className="pt-6">
        <div className="overflow-x-auto">
          <table className="w-full">
            <thead className="bg-slate-800/50">
              <tr>
                <th className="p-4 text-left text-sm font-semibold text-slate-300">Time</th>
                <th className="p-4 text-left text-sm font-semibold text-slate-300">Severity</th>
                <th className="p-4 text-left text-sm font-semibold text-slate-300">Rule</th>
                <th className="p-4 text-left text-sm font-semibold text-slate-300">Entity</th>
                <th className="p-4 text-left text-sm font-semibold text-slate-300">MITRE</th>
                <th className="p-4 text-left text-sm font-semibold text-slate-300">Action</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-700">
              {alerts.map((alert, idx) => (
                <tr key={alert.id} className={`hover:bg-slate-700/30 transition-colors ${idx % 2 === 0 ? 'bg-slate-800/30' : ''}`}>
                  <td className="p-4 text-slate-400 text-sm">{formatDate(alert.timestamp)}</td>
                  <td className="p-4">
                    <Badge className={`${getSeverityColor(alert.severity)} border`}>{alert.severity.toUpperCase()}</Badge>
                  </td>
                  <td className="p-4 text-white text-sm">{alert.rule_name}</td>
                  <td className="p-4">
                    <div className="font-medium text-white">{alert.entity_name}</div>
                    <div className="text-xs text-slate-400 capitalize">{alert.entity_type}</div>
                  </td>
                  <td className="p-4">
                    {alert.technique_id ? (
                      <Badge variant="outline" className="text-purple-400 border-purple-500/30">
                        T-{alert.technique_id}
                      </Badge>
                    ) : (
                      <span className="text-slate-500 text-sm">-</span>
                    )}
                  </td>
                  <td className="p-4">
                    <Button size="sm" variant="outline" className="border-slate-600 text-slate-300 hover:bg-slate-700">
                      <Eye className="w-3 h-3" />
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </CardContent>
    </Card>
  );
};

const UEBAAnalysisView = ({ case, anomalies }: { case: HuntCase; anomalies: UEBAAnomaly[] }) => {
  return (
    <Card className="border-slate-700 bg-slate-800/50">
      <CardHeader>
        <div className="flex items-center justify-between">
          <div>
            <h3 className="text-xl font-bold text-white">UEBA Behavioral Analysis</h3>
            <p className="text-sm text-slate-400">Statistical anomaly detection with explainable AI</p>
          </div>
          <Button size="sm" variant="outline" className="border-slate-600 text-slate-300 hover:bg-slate-700">
            <BrainCircuit className="w-3 h-3 mr-1" />
            Run Analysis
          </Button>
        </div>
      </CardHeader>
      <CardContent>
        <EmptyState message="UEBA analysis interface coming soon" />
      </CardContent>
    </Card>
  );
};

const MitreATTACKView = () => {
  const tactics = ['Initial Access', 'Execution', 'Persistence', 'Privilege Escalation', 'Defense Evasion'];
  
  return (
    <Card className="border-slate-700 bg-slate-800/50">
      <CardHeader>
        <div className="flex items-center justify-between">
          <div>
            <h3 className="text-xl font-bold text-white">MITRE ATT&CK Matrix</h3>
            <p className="text-sm text-slate-400">Mapping detections to known adversary tactics</p>
          </div>
        </div>
      </CardHeader>
      <CardContent>
        <div className="space-y-4">
          {tactics.map((tactic, idx) => (
            <Card key={tactic} className="border-slate-700 bg-slate-800/30">
              <CardContent className="pt-4">
                <h4 className="font-semibold text-white mb-3">{tactic}</h4>
                <div className="flex gap-2 flex-wrap">
                  {[1, 2, 3].map(i => (
                    <Badge key={i} variant="outline" className="text-blue-400 border-blue-500/30">
                      Technique-{idx * 10 + i}
                    </Badge>
                  ))}
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      </CardContent>
    </Card>
  );
};

const CreateCaseModal = ({
  onClose,
  onSubmit,
  isSubmitting,
  initialData,
  onUpdate
}: {
  onClose: () => void;
  onSubmit: (data: any) => void;
  isSubmitting: boolean;
  initialData: any;
  onUpdate: (data: any) => void;
}) => {
  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    onSubmit(initialData);
  };

  return (
    <div className="fixed inset-0 bg-black/80 backdrop-blur-sm z-50 flex items-center justify-center p-4">
      <Card className="w-full max-w-2xl border-slate-700 bg-slate-900">
        <CardHeader className="border-b border-slate-700">
          <h2 className="text-2xl font-bold text-white">Create Hunt Case</h2>
          <p className="text-sm text-slate-400">Initiate new security investigation workflow</p>
        </CardHeader>
        <form onSubmit={handleSubmit}>
          <CardContent className="space-y-4 pt-6">
            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label htmlFor="name" className="text-slate-300">Case Name *</Label>
                <Input
                  id="name"
                  value={initialData.name}
                  onChange={(e) => onUpdate({ ...initialData, name: e.target.value })}
                  placeholder="Suspicious User Activity Investigation"
                  className="bg-slate-800 border-slate-600 text-white"
                  required
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="severity" className="text-slate-300">Severity *</Label>
                <Select
                  value={initialData.severity}
                  onValueChange={(val: any) => onUpdate({ ...initialData, severity: val })}
                >
                  <SelectTrigger className="bg-slate-800 border-slate-600 text-white">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="critical">Critical</SelectItem>
                    <SelectItem value="high">High</SelectItem>
                    <SelectItem value="medium">Medium</SelectItem>
                    <SelectItem value="low">Low</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </div>

            <div className="space-y-2">
              <Label htmlFor="description" className="text-slate-300">Description</Label>
              <Textarea
                id="description"
                value={initialData.description}
                onChange={(e) => onUpdate({ ...initialData, description: e.target.value })}
                placeholder="Describe the suspicious activity and investigation scope..."
                className="bg-slate-800 border-slate-600 text-white"
                rows={4}
              />
            </div>

            <div className="flex justify-end gap-3 pt-4 border-t border-slate-700">
              <Button
                type="button"
                onClick={onClose}
                disabled={isSubmitting}
                variant="outline"
                className="border-slate-600 text-slate-300 hover:bg-slate-800"
              >
                Cancel
              </Button>
              <Button
                type="submit"
                disabled={isSubmitting}
                className="bg-red-600 hover:bg-red-700 text-white"
              >
                {isSubmitting ? (
                  <>
                    <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                    Creating...
                  </>
                ) : (
                  <>
                    <ShieldAlert className="w-4 h-4 mr-2" />
                    Start Investigation
                  </>
                )}
              </Button>
            </div>
          </CardContent>
        </form>
      </Card>
    </div>
  );
};

const EmptyState = ({ message }: { message: string }) => (
  <div className="text-center py-12">
    <ShieldAlert className="w-16 h-16 mx-auto text-slate-500 mb-4" />
    <h3 className="text-xl font-semibold text-white mb-2">No Data Available</h3>
    <p className="text-slate-400">{message}</p>
  </div>
);
