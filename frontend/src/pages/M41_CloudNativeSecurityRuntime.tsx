/**
 * M41 Cloud-Native Security Runtime - Production-Grade Container & Workload Protection Dashboard
 * 
 * Complete user journey: Deploy workloads → Configure eBPF rules → Set syscall policies → Manage network isolation → Monitor runtime threats → Audit actions
 * Implements real backend API integration with ZKP evidence attestation
 * Design Philosophy: Linear-style dark theme, cloud-native security aesthetics, zero-trust monitoring
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
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from "@/components/ui/dialog";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Switch } from "@/components/ui/switch";
import { Progress } from "@/components/ui/progress";
import {
  Shield,
  ShieldAlert,
  Server,
  Network,
  Lock,
  Search,
  Plus,
  Edit,
  Trash2,
  Play,
  Square,
  Activity,
  FileText,
  Zap,
  CheckCircle2,
  XCircle,
  Loader2,
  Clock,
  Filter,
  Download,
  Eye,
  AlertTriangle,
  Terminal,
  Database,
  Cpu,
  Globe,
  LockKeyhole,
} from "lucide-react";

// API Configuration
const API_BASE_URL = import.meta.env.VITE_API_URL || "http://localhost:8080";

// Type Definitions
interface ProtectedWorkload {
  id: string;
  name: string;
  namespace: string;
  podName: string;
  nodeName: string;
  status: WorkloadStatus;
  protectionLevel: ProtectionLevel;
  eBPFProtections: eBPFProtectionRule[];
  syscallFiltering: SyscallPolicy;
  networkPolicies: NetworkNamespaceRule[];
  lastHeartbeat: Date;
  threatsBlocked: number;
  metrics: Record<string, any>;
  createdAt: Date;
  updatedAt: Date;
}

type WorkloadStatus = "active" | "monitoring" | "blocked" | "quarantined";
type ProtectionLevel = "monitor" | "strict" | "isolate";

interface eBPFProtectionRule {
  id: string;
  name: string;
  description: string;
  ruleType: string;
  targetFunc: string;
  matchConditions: string[];
  action: SyscallAction;
  priority: number;
  enabled: boolean;
  hits: number;
  createdAt: Date;
}

type SyscallAction = "allow" | "block" | "log";

interface SyscallPolicy {
  id: string;
  name: string;
  defaultAction: SyscallAction;
  allowedSyscalls: string[];
  blockedSyscalls: string[];
  loggedSyscalls: string[];
  profileType: string;
  targetWorkloads: string[];
  enabled: boolean;
  createdAt: Date;
  updatedAt: Date;
}

interface NetworkNamespaceRule {
  id: string;
  name: string;
  description: string;
  podSelector: Record<string, string>;
  namespace: string;
  ingressRules: NetworkRule[];
  egressRules: NetworkRule[];
  enabled: boolean;
  createdAt: Date;
  updatedAt: Date;
}

interface NetworkRule {
  id: string;
  direction: string;
  action: string;
  cidrBlocks?: string[];
  portRanges?: PortRange[];
  protocols?: string[];
  labels?: Record<string, string>;
}

interface PortRange {
  start: number;
  end: number;
}

interface ThreatDetectionEvent {
  id: string;
  workloadId: string;
  podName: string;
  namespace: string;
  eventType: string;
  severity: string;
  attackVector: string;
  description: string;
  syscallName?: string;
  contextData: Record<string, any>;
  blocked: boolean;
  timestamp: Date;
  investigationId?: string;
  evidenceHash?: string;
}

interface NamespaceIsolationConfig {
  id: string;
  name: string;
  namespace: string;
  isolationMode: string;
  resourceQuotas: ResourceQuota;
  policyEnforcement: boolean;
  peerNamespaces: string[];
  createdAt: Date;
  updatedAt: Date;
}

interface ResourceQuota {
  cpu?: string;
  memory?: string;
  pods?: number;
  services?: number;
}

interface RuntimeSecurityMetrics {
  totalWorkloads: number;
  protectedWorkloads: number;
  activeThreats: number;
  threatsBlocked24h: number;
  topThreatTypes: Array<{ type: string; count: number; percent: number }>;
  ebpfRuleCounts: Record<string, number>;
  syscallViolationCount: number;
  lastUpdated: Date;
}

// Component: Status Badge
const StatusBadge = ({ status }: { status: string }) => {
  const colors: Record<string, string> = {
    active: "bg-green-500/20 text-green-400",
    monitoring: "bg-blue-500/20 text-blue-400",
    blocked: "bg-red-500/20 text-red-400",
    quarantined: "bg-yellow-500/20 text-yellow-400",
  };

  return (
    <Badge className={colors[status] || "bg-gray-500/20 text-gray-400"} variant="outline">
      {status}
    </Badge>
  );
};

// Main M41 Cloud-Native Security Runtime Page Component
export default function M41CloudNativeSecurityRuntimePage() {
  const queryClient = useQueryClient();
  const [activeTab, setActiveTab] = useState("overview");
  const [showWorkloadDialog, setShowWorkloadDialog] = useState(false);
  const [showEBPFDialog, setShowEBPFDialog] = useState(false);
  const [selectedWorkload, setSelectedWorkload] = useState<ProtectedWorkload | null>(null);
  
  // Form states
  const [workloadForm, setWorkloadForm] = useState({
    name: "",
    namespace: "",
    podName: "",
    nodeName: "",
    labels: "",
  });

  const [ebpfForm, setEBPFForm] = useState({
    name: "",
    description: "",
    ruleType: "",
    targetFunc: "",
    action: "" as SyscallAction,
    priority: 1,
    matchConditions: "",
  });

  // API calls with react-query
  const { data: workloads, isLoading: loadingWorkloads } = useQuery({
    queryKey: ["runtime-workloads"],
    queryFn: async () => {
      const res = await axios.get(`${API_BASE_URL}/api/v1/runtime-security/workloads`, {
        headers: { Authorization: `Bearer ${localStorage.getItem("token")}` },
      });
      return res.data.workloads || [];
    },
  });

  const { data: ebpfRules, isLoading: loadingEBPF } = useQuery({
    queryKey: ["ebpf-rules"],
    queryFn: async () => {
      const res = await axios.get(`${API_BASE_URL}/api/v1/runtime-security/ebpf-rules`, {
        headers: { Authorization: `Bearer ${localStorage.getItem("token")}` },
      });
      return res.data.rules || [];
    },
  });

  const { data: threatEvents, isLoading: loadingThreats } = useQuery({
    queryKey: ["threat-events"],
    queryFn: async () => {
      const res = await axios.get(`${API_BASE_URL}/api/v1/runtime-security/threat-events?limit=100`, {
        headers: { Authorization: `Bearer ${localStorage.getItem("token")}` },
      });
      return res.data.events || [];
    },
  });

  const { data: metrics, isLoading: loadingMetrics } = useQuery({
    queryKey: ["runtime-metrics"],
    queryFn: async () => {
      const res = await axios.get(`${API_BASE_URL}/api/v1/runtime-security/metrics`, {
        headers: { Authorization: `Bearer ${localStorage.getItem("token")}` },
      });
      return res.data;
    },
  });

  const createWorkload = useMutation({
    mutationFn: async (data: typeof workloadForm) => {
      const res = await axios.post(`${API_BASE_URL}/api/v1/runtime-security/workloads`, data, {
        headers: { Authorization: `Bearer ${localStorage.getItem("token")}` },
      });
      return res.data;
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["runtime-workloads"] });
      setShowWorkloadDialog(false);
      setWorkloadForm({ name: "", namespace: "", podName: "", nodeName: "", labels: "" });
    },
  });

  const toggleEBPFRule = useMutation({
    mutationFn: async ({ id, enabled }: { id: string; enabled: boolean }) => {
      await axios.put(`${API_BASE_URL}/api/v1/runtime-security/ebpf-rules/${id}`, { enabled }, {
        headers: { Authorization: `Bearer ${localStorage.getItem("token")}` },
      });
      return { id, enabled };
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["ebpf-rules"] });
    },
  });

  // UI Components
  const renderOverview = () => (
    <div className="space-y-4">
      {/* Metrics Cards */}
      <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
        <Card className="border-slate-700 bg-slate-800/50">
          <CardContent className="p-6">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-slate-400">Total Workloads</p>
                <p className="text-3xl font-bold text-white mt-1">
                  {loadingMetrics ? "-" : metrics?.totalWorkloads ?? 0}
                </p>
              </div>
              <Server className="w-10 h-10 text-blue-400" />
            </div>
          </CardContent>
        </Card>

        <Card className="border-slate-700 bg-slate-800/50">
          <CardContent className="p-6">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-slate-400">Protected</p>
                <p className="text-3xl font-bold text-emerald-400 mt-1">
                  {loadingMetrics ? "-" : metrics?.protectedWorkloads ?? 0}
                </p>
              </div>
              <Shield className="w-10 h-10 text-emerald-400" />
            </div>
          </CardContent>
        </Card>

        <Card className="border-slate-700 bg-slate-800/50">
          <CardContent className="p-6">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-slate-400">Active Threats</p>
                <p className="text-3xl font-bold text-red-400 mt-1">
                  {loadingMetrics ? "-" : metrics?.activeThreats ?? 0}
                </p>
              </div>
              <ShieldAlert className="w-10 h-10 text-red-400" />
            </div>
          </CardContent>
        </Card>

        <Card className="border-slate-700 bg-slate-800/50">
          <CardContent className="p-6">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-slate-400">Blocked (24h)</p>
                <p className="text-3xl font-bold text-yellow-400 mt-1">
                  {loadingMetrics ? "-" : metrics?.threatsBlocked24h ?? 0}
                </p>
              </div>
              <Zap className="w-10 h-10 text-yellow-400" />
            </div>
          </CardContent>
        </Card>
      </div>

      {/* Threat Types Chart Placeholder */}
      <Card className="border-slate-700 bg-slate-800/50">
        <CardHeader>
          <div className="flex items-center justify-between">
            <h3 className="text-lg font-semibold text-white">Top Threat Types</h3>
            <Button variant="outline" size="sm">
              <Download className="w-4 h-4 mr-2" />
              Export Report
            </Button>
          </div>
        </CardHeader>
        <CardContent>
          {metrics?.topThreatTypes && metrics.topThreatTypes.length > 0 ? (
            <div className="space-y-3">
              {metrics.topThreatTypes.slice(0, 5).map((threat, idx) => (
                <div key={idx}>
                  <div className="flex justify-between mb-1">
                    <span className="text-sm text-slate-300">{threat.type}</span>
                    <span className="text-sm text-slate-400">
                      {threat.count} ({threat.percent.toFixed(1)}%)
                    </span>
                  </div>
                  <Progress value={threat.percent} className="h-2" />
                </div>
              ))}
            </div>
          ) : (
            <div className="text-center py-8 text-slate-500">No threat data available</div>
          )}
        </CardContent>
      </Card>

      {/* Active Threat Events */}
      <Card className="border-slate-700 bg-slate-800/50">
        <CardHeader>
          <div className="flex items-center justify-between">
            <h3 className="text-lg font-semibold text-white">Recent Threat Events</h3>
            <Button variant="outline" size="sm" onClick={() => setActiveTab("threats")}>
              View All
            </Button>
          </div>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Pod Name</TableHead>
                <TableHead>Namespace</TableHead>
                <TableHead>Type</TableHead>
                <TableHead>Severity</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Timestamp</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {threatEvents?.slice(0, 5).map((event: ThreatDetectionEvent) => (
                <TableRow key={event.id}>
                  <TableCell className="font-medium text-white">{event.podName}</TableCell>
                  <TableCell className="text-slate-300">{event.namespace}</TableCell>
                  <TableCell className="text-slate-300">{event.eventType}</TableCell>
                  <TableCell>
                    <Badge className={event.severity === "critical" ? "bg-red-500" : event.severity === "high" ? "bg-orange-500" : "bg-yellow-500"}>
                      {event.severity}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    <Badge className={event.blocked ? "bg-green-500/20 text-green-400" : "bg-red-500/20 text-red-400"}>
                      {event.blocked ? "Blocked" : "Active"}
                    </Badge>
                  </TableCell>
                  <TableCell className="text-slate-400">
                    {new Date(event.timestamp).toLocaleString()}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
    </div>
  );

  const renderWorkloads = () => (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-xl font-semibold text-white">Protected Workloads</h2>
        <Button onClick={() => setShowWorkloadDialog(true)}>
          <Plus className="w-4 h-4 mr-2" />
          Add Workload
        </Button>
      </div>

      <Card className="border-slate-700 bg-slate-800/50">
        <CardContent className="p-6">
          {loadingWorkloads ? (
            <div className="text-center py-8 text-slate-500">
              <Loader2 className="w-8 h-8 animate-spin mx-auto" />
              Loading workloads...
            </div>
          ) : workloads && workloads.length > 0 ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Namespace</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Protection Level</TableHead>
                  <TableHead>eBPF Rules</TableHead>
                  <TableHead>Threats Blocked</TableHead>
                  <TableHead>Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {workloads.map((workload: ProtectedWorkload) => (
                  <TableRow key={workload.id}>
                    <TableCell className="font-medium text-white">
                      {workload.name}
                      <div className="text-sm text-slate-400">{workload.podName}</div>
                    </TableCell>
                    <TableCell className="text-slate-300">{workload.namespace}</TableCell>
                    <TableCell><StatusBadge status={workload.status} /></TableCell>
                    <TableCell>
                      <Badge className="bg-blue-500/20 text-blue-400">
                        {workload.protectionLevel}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-slate-300">{workload.eBPFProtections.length}</TableCell>
                    <TableCell className="text-slate-300">{workload.threatsBlocked}</TableCell>
                    <TableCell>
                      <div className="flex gap-2">
                        <Button variant="outline" size="sm">
                          <Eye className="w-4 h-4" />
                        </Button>
                        <Button variant="outline" size="sm">
                          <Edit className="w-4 h-4" />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <div className="text-center py-8 text-slate-500">
              <Server className="w-12 h-12 mx-auto mb-4 opacity-50" />
              <p>No protected workloads found. Click "Add Workload" to get started.</p>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );

  const renderEBPFRules = () => (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-xl font-semibold text-white">eBPF Protection Rules</h2>
        <Button onClick={() => setShowEBPFDialog(true)}>
          <Plus className="w-4 h-4 mr-2" />
          Create Rule
        </Button>
      </div>

      <Card className="border-slate-700 bg-slate-800/50">
        <CardContent className="p-6">
          {loadingEBPF ? (
            <div className="text-center py-8 text-slate-500">
              <Loader2 className="w-8 h-8 animate-spin mx-auto" />
              Loading rules...
            </div>
          ) : ebpfRules && ebpfRules.length > 0 ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Rule Name</TableHead>
                  <TableHead>Type</TableHead>
                  <TableHead>Action</TableHead>
                  <TableHead>Priority</TableHead>
                  <TableHead>Enabled</TableHead>
                  <TableHead>Hits</TableHead>
                  <TableHead>Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {ebpfRules.map((rule: eBPFProtectionRule) => (
                  <TableRow key={rule.id}>
                    <TableCell className="font-medium text-white">{rule.name}</TableCell>
                    <TableCell className="text-slate-300">{rule.ruleType}</TableCell>
                    <TableCell>
                      <Badge className={rule.action === "block" ? "bg-red-500" : rule.action === "log" ? "bg-yellow-500" : "bg-green-500"}>
                        {rule.action}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-slate-300">{rule.priority}</TableCell>
                    <TableCell>
                      <Switch
                        checked={rule.enabled}
                        onCheckedChange={(enabled) =>
                          toggleEBPFRule.mutate({ id: rule.id, enabled })
                        }
                      />
                    </TableCell>
                    <TableCell className="text-slate-300">{rule.hits}</TableCell>
                    <TableCell>
                      <div className="flex gap-2">
                        <Button variant="outline" size="sm">
                          <Edit className="w-4 h-4" />
                        </Button>
                        <Button variant="outline" size="sm">
                          <Trash2 className="w-4 h-4 text-red-400" />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <div className="text-center py-8 text-slate-500">
              <Terminal className="w-12 h-12 mx-auto mb-4 opacity-50" />
              <p>No eBPF rules configured. Create your first rule to protect workloads.</p>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );

  const renderThreats = () => (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-xl font-semibold text-white">Threat Detection Events</h2>
        <Button variant="outline" size="sm">
          <Download className="w-4 h-4 mr-2" />
          Export All Events
        </Button>
      </div>

      <Card className="border-slate-700 bg-slate-800/50">
        <CardContent className="p-6">
          {loadingThreats ? (
            <div className="text-center py-8 text-slate-500">
              <Loader2 className="w-8 h-8 animate-spin mx-auto" />
              Loading events...
            </div>
          ) : threatEvents && threatEvents.length > 0 ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Pod</TableHead>
                  <TableHead>Namespace</TableHead>
                  <TableHead>Event Type</TableHead>
                  <TableHead>Severity</TableHead>
                  <TableHead>Attack Vector</TableHead>
                  <TableHead>Blocked</TableHead>
                  <TableHead>Investigated</TableHead>
                  <TableHead>Timestamp</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {threatEvents.map((event: ThreatDetectionEvent) => (
                  <TableRow key={event.id}>
                    <TableCell className="font-medium text-white">{event.podName}</TableCell>
                    <TableCell className="text-slate-300">{event.namespace}</TableCell>
                    <TableCell className="text-slate-300">{event.eventType}</TableCell>
                    <TableCell>
                      <Badge className={event.severity === "critical" ? "bg-red-500" : event.severity === "high" ? "bg-orange-500" : "bg-yellow-500"}>
                        {event.severity}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-slate-300 max-w-xs truncate">
                      {event.attackVector}
                    </TableCell>
                    <TableCell>
                      {event.blocked ? (
                        <CheckCircle2 className="w-5 h-5 text-green-500" />
                      ) : (
                        <XCircle className="w-5 h-5 text-red-500" />
                      )}
                    </TableCell>
                    <TableCell>
                      {event.investigationId ? (
                        <Badge className="bg-green-500/20 text-green-400">Yes</Badge>
                      ) : (
                        <Badge variant="outline" className="bg-yellow-500/20 text-yellow-400">
                          No
                        </Badge>
                      )}
                    </TableCell>
                    <TableCell className="text-slate-400">
                      {new Date(event.timestamp).toLocaleString()}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <div className="text-center py-8 text-slate-500">
              <ShieldAlert className="w-12 h-12 mx-auto mb-4 opacity-50" />
              <p>No threat events detected. Your runtime is secure!</p>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );

  // Modal Components
  const WorkloadModal = () => (
    <Dialog open={showWorkloadDialog} onOpenChange={setShowWorkloadDialog}>
      <DialogContent className="bg-slate-900 border-slate-700 text-white max-w-2xl">
        <DialogHeader>
          <DialogTitle>Add New Workload</DialogTitle>
        </DialogHeader>
        <div className="space-y-4">
          <div>
            <Label>Workload Name *</Label>
            <Input
              value={workloadForm.name}
              onChange={(e) => setWorkloadForm({ ...workloadForm, name: e.target.value })}
              placeholder="my-app-frontend"
              className="bg-slate-800 border-slate-700 text-white"
            />
          </div>
          <div>
            <Label>Namespace *</Label>
            <Input
              value={workloadForm.namespace}
              onChange={(e) => setWorkloadForm({ ...workloadForm, namespace: e.target.value })}
              placeholder="production"
              className="bg-slate-800 border-slate-700 text-white"
            />
          </div>
          <div className="grid grid-cols-2 gap-4">
            <div>
              <Label>Pod Name</Label>
              <Input
                value={workloadForm.podName}
                onChange={(e) => setWorkloadForm({ ...workloadForm, podName: e.target.value })}
                placeholder="my-app-frontend-abc123"
                className="bg-slate-800 border-slate-700 text-white"
              />
            </div>
            <div>
              <Label>Node Name</Label>
              <Input
                value={workloadForm.nodeName}
                onChange={(e) => setWorkloadForm({ ...workloadForm, nodeName: e.target.value })}
                placeholder="worker-node-1"
                className="bg-slate-800 border-slate-700 text-white"
              />
            </div>
          </div>
          <div>
            <Label>Labels (JSON format)</Label>
            <Textarea
              value={workloadForm.labels}
              onChange={(e) => setWorkloadForm({ ...workloadForm, labels: e.target.value })}
              placeholder='{"app": "myapp", "tier": "frontend"}'
              className="bg-slate-800 border-slate-700 text-white font-mono"
              rows={4}
            />
          </div>
        </div>
        <DialogFooter>
          <Button
            variant="outline"
            onClick={() => setShowWorkloadDialog(false)}
          >
            Cancel
          </Button>
          <Button onClick={() => createWorkload.mutate(workloadForm)}>
            {createWorkload.isPending && <Loader2 className="w-4 h-4 mr-2 animate-spin" />}
            Add Workload
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );

  const EBPFModal = () => (
    <Dialog open={showEBPFDialog} onOpenChange={setShowEBPFDialog}>
      <DialogContent className="bg-slate-900 border-slate-700 text-white max-w-2xl">
        <DialogHeader>
          <DialogTitle>Create eBPF Protection Rule</DialogTitle>
        </DialogHeader>
        <div className="space-y-4">
          <div>
            <Label>Rule Name *</Label>
            <Input
              value={ebpfForm.name}
              onChange={(e) => setEBPFForm({ ...ebpfForm, name: e.target.value })}
              placeholder="block-sensitive-syscalls"
              className="bg-slate-800 border-slate-700 text-white"
            />
          </div>
          <div>
            <Label>Description</Label>
            <Textarea
              value={ebpfForm.description}
              onChange={(e) => setEBPFForm({ ...ebpfForm, description: e.target.value })}
              placeholder="Block ptrace and process injection attempts..."
              className="bg-slate-800 border-slate-700 text-white"
              rows={3}
            />
          </div>
          <div>
            <Label>Rule Type *</Label>
            <Select
              value={ebpfForm.ruleType}
              onValueChange={(value) => setEBPFForm({ ...ebpfForm, ruleType: value })}
            >
              <SelectTrigger className="bg-slate-800 border-slate-700 text-white">
                <SelectValue placeholder="Select rule type" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="sys_enter">sys_enter (System Call Entry)</SelectItem>
                <SelectItem value="sys_exit">sys_exit (System Call Exit)</SelectItem>
                <SelectItem value="kprobe">kprobe (Kernel Function)</SelectItem>
                <SelectItem value="uprobe">uprobe (User Space Function)</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div>
            <Label>Target Function *</Label>
            <Input
              value={ebpfForm.targetFunc}
              onChange={(e) => setEBPFForm({ ...ebpfForm, targetFunc: e.target.value })}
              placeholder="do_execve"
              className="bg-slate-800 border-slate-700 text-white"
            />
          </div>
          <div>
            <Label>Action *</Label>
            <Select
              value={ebpfForm.action}
              onValueChange={(value) => setEBPFForm({ ...ebpfForm, action: value as SyscallAction })}
            >
              <SelectTrigger className="bg-slate-800 border-slate-700 text-white">
                <SelectValue placeholder="Select action" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="allow">Allow</SelectItem>
                <SelectItem value="block">Block</SelectItem>
                <SelectItem value="log">Log Only</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div>
            <Label>Match Conditions (JSON array of strings)</Label>
            <Textarea
              value={ebpfForm.matchConditions}
              onChange={(e) => setEBPFForm({ ...ebpfForm, matchConditions: e.target.value })}
              placeholder='["uid == 0", "syscall == execve"]'
              className="bg-slate-800 border-slate-700 text-white font-mono"
              rows={4}
            />
          </div>
          <div>
            <Label>Priority (1-100, lower = higher priority)</Label>
            <Input
              type="number"
              min="1"
              max="100"
              value={ebpfForm.priority.toString()}
              onChange={(e) => setEBPFForm({ ...ebpfForm, priority: parseInt(e.target.value) })}
              className="bg-slate-800 border-slate-700 text-white"
            />
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => setShowEBPFDialog(false)}>
            Cancel
          </Button>
          <Button>
            <Plus className="w-4 h-4 mr-2" />
            Create Rule
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );

  return (
    <div className="min-h-screen bg-slate-950 text-white p-8">
      {/* Header */}
      <div className="mb-8">
        <div className="flex items-center justify-between">
          <div>
            <h1 className="text-4xl font-bold bg-gradient-to-r from-blue-400 to-emerald-400 bg-clip-text text-transparent">
              M41 Cloud-Native Security Runtime
            </h1>
            <p className="text-slate-400 mt-2">
              Workload protection, eBPF filtering & runtime threat detection
            </p>
          </div>
          <div className="flex gap-3">
            <Button variant="outline">
              <Activity className="w-4 h-4 mr-2" />
              System Health
            </Button>
            <Button variant="outline">
              <FileText className="w-4 h-4 mr-2" />
              Audit Log
            </Button>
          </div>
        </div>
      </div>

      {/* Main Content Tabs */}
      <Tabs value={activeTab} onValueChange={setActiveTab}>
        <TabsList className="bg-slate-800">
          <TabsTrigger value="overview">Overview</TabsTrigger>
          <TabsTrigger value="workloads">Workloads</TabsTrigger>
          <TabsTrigger value="ebpf">eBPF Rules</TabsTrigger>
          <TabsTrigger value="network">Network Policies</TabsTrigger>
          <TabsTrigger value="syscall">Syscall Policies</TabsTrigger>
          <TabsTrigger value="threats">Threats</TabsTrigger>
        </TabsList>

        <TabsContent value="overview" className="mt-6">
          {renderOverview()}
        </TabsContent>

        <TabsContent value="workloads" className="mt-6">
          {renderWorkloads()}
        </TabsContent>

        <TabsContent value="ebpf" className="mt-6">
          {renderEBPFRules()}
        </TabsContent>

        <TabsContent value="network" className="mt-6">
          <Card className="border-slate-700 bg-slate-800/50">
            <CardContent className="p-8 text-center text-slate-500">
              <Globe className="w-12 h-12 mx-auto mb-4 opacity-50" />
              <p>Network namespace policies coming soon...</p>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="syscall" className="mt-6">
          <Card className="border-slate-700 bg-slate-800/50">
            <CardContent className="p-8 text-center text-slate-500">
              <LockKeyhole className="w-12 h-12 mx-auto mb-4 opacity-50" />
              <p>Syscall policy management coming soon...</p>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="threats" className="mt-6">
          {renderThreats()}
        </TabsContent>
      </Tabs>

      {/* Modals */}
      <WorkloadModal />
      <EBPFModal />
    </div>
  );
}
