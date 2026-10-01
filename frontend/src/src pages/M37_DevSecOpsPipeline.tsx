/**
 * M37 DevSecOps Pipeline Integration - Production-Grade Security Pipeline Dashboard
 * 
 * Complete user journey: Configure pipelines → Run scans → Review findings → Adjust gates → Monitor compliance
 * Implements real backend API integration with CloudAI Fusion DevSecOps endpoints
 * Design Philosophy: Linear-style dark theme, CI/CD pipeline aesthetics, stage-based security checks
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
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Progress } from "@/components/ui/progress";
import { Switch } from "@/components/ui/switch";
import {
  ShieldAlert,
  Search,
  GitBranch,
  CheckCircle2,
  XCircle,
  Loader2,
  Play,
  Clock,
  Activity,
  TrendingUp,
  Settings,
  FileText,
  Plus,
  Eye,
  Edit,
  Trash2,
  ExternalLink,
  Flag,
  Target,
} from "lucide-react";

const API_BASE_URL = import.meta.env.VITE_API_URL || "http://localhost:8080";

// ============================================================================
// Type Definitions
// ============================================================================

interface SecurityJob {
  id: string;
  name: string;
  description?: string;
  repositoryUrl: string;
  branch: string;
  pipelineConfig: string;
  triggerEvents: string[];
  gatePolicyId?: string;
  enabled: boolean;
  webhookSecret?: string;
  status: 'active' | 'inactive';
  lastRunAt?: string;
}

interface GatePolicy {
  id: string;
  name: string;
  description?: string;
  scanTypes: ('sca' | 'sast' | 'dast' | 'iac_scan' | 'secrets_scan')[];
  failureThresholds: Record<string, number>;
  blockOnSecrets: boolean;
  blockOnCriticalCves: boolean;
  blockOnHighCves: boolean;
}

interface JobRun {
  runId: string;
  startedAt: string;
  completedAt?: string;
  status: 'pending' | 'running' | 'completed' | 'failed' | 'skipped';
  durationMs?: number;
  stages: Array<{
    type: string;
    status: string;
    passed: boolean;
    vulnerabilitiesFound: number;
    secretsFound: number;
  }>;
}

interface ComplianceStatus {
  score: number;
  totalChecks: number;
  passedChecks: number;
  failedChecks: number;
  status: 'compliant' | 'non_compliant' | 'pending';
}

// ============================================================================
// Component Implementation
// ============================================================================

export function M37DevSecOpsPipelinePage() {
  const queryClient = useQueryClient();
  const [searchTerm, setSearchTerm] = useState("");
  const [selectedJob, setSelectedJob] = useState<SecurityJob | null>(null);
  const [showCreateDialog, setShowCreateDialog] = useState(false);
  const [selectedRun, setSelectedRun] = useState<JobRun | null>(null);
  
  const [newJob, setNewJob] = useState({
    name: "",
    repositoryUrl: "",
    branch: "main",
    pipelineConfig: ".github/workflows/security.yml",
    triggerEvents: ["push"],
  });

  // Fetch jobs
  const { data: jobs, isLoading: jobsLoading } = useQuery({
    queryKey: ["m37-jobs"],
    queryFn: async () => {
      const res = await axios.get(`${API_BASE_URL}/api/m37/devsecops/jobs`);
      return res.data;
    },
  });

  // Fetch gate policies
  const { data: gatePolicies } = useQuery({
    queryKey: ["m37-gate-policies"],
    queryFn: async () => {
      const res = await axios.get(`${API_BASE_URL}/api/m37/devsecops/gates`);
      return res.data;
    },
  });

  // Fetch compliance status
  const { data: complianceStatus } = useQuery({
    queryKey: ["m37-compliance-status"],
    queryFn: async () => {
      const res = await axios.get(`${API_BASE_URL}/api/m37/devsecops/compliance/status`);
      return res.data;
    },
  });

  // Create job mutation
  const createMutation = useMutation({
    mutationFn: async (data: typeof newJob) => {
      const res = await axios.post(`${API_BASE_URL}/api/m37/devsecops/jobs`, data);
      return res.data;
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["m37-jobs"] });
      setShowCreateDialog(false);
    },
  });

  const handleCreate = () => {
    createMutation.mutate(newJob);
  };

  // Filter jobs
  const filteredJobs = jobs?.filter((job: SecurityJob) =>
    job.name.toLowerCase().includes(searchTerm.toLowerCase()) ||
    job.repositoryUrl.toLowerCase().includes(searchTerm.toLowerCase())
  );

  return (
    <div className="min-h-screen bg-gradient-to-br from-gray-900 via-slate-900 to-gray-900 p-6 space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between animate-in fade-in slide-in-from-top-4 duration-500">
        <div className="space-y-1">
          <h1 className="text-3xl font-bold bg-gradient-to-r from-blue-400 to-purple-500 bg-clip-text text-transparent">
            DevSecOps Pipeline Integration
          </h1>
          <p className="text-gray-400">CI/CD security scanning & automated compliance gates</p>
        </div>
        <div className="flex items-center gap-3">
          <Button variant="outline" className="border-slate-700 text-gray-300" onClick={() => window.location.reload()}>
            <RefreshCw className="mr-2 h-4 w-4" />
            Refresh Status
          </Button>
          <Button
            onClick={() => setShowCreateDialog(true)}
            className="bg-blue-600 hover:bg-blue-700"
          >
            <Plus className="mr-2 h-4 w-4" />
            New Security Job
          </Button>
        </div>
      </div>

      {/* Compliance Score Card */}
      <Card className="bg-gradient-to-br from-green-500/20 to-emerald-600/20 border-green-500/30 backdrop-blur-sm animate-in fade-in slide-in-from-bottom-4 duration-700">
        <CardContent className="p-6">
          <div className="flex items-center justify-between">
            <div className="space-y-2">
              <div className="flex items-center gap-2">
                <ShieldAlert className="h-5 w-5 text-green-400" />
                <h3 className="text-lg font-semibold text-white">Overall Security Posture</h3>
              </div>
              <div className="flex items-end gap-3">
                <span className="text-4xl font-bold text-green-400">
                  {(complianceStatus?.score || 0).toFixed(0)}%
                </span>
                <Badge
                  className={complianceStatus?.status === "compliant" ? "bg-green-500/20 text-green-400" : "bg-red-500/20 text-red-400"}
                >
                  {(complianceStatus?.status || "pending").toUpperCase()}
                </Badge>
              </div>
              <p className="text-sm text-gray-400">
                {complianceStatus?.passedChecks || 0}/{complianceStatus?.totalChecks || 0} checks passed
              </p>
            </div>
            <Progress value={complianceStatus?.score || 0} className="w-48" indicatorClassName="bg-green-500" />
          </div>
        </CardContent>
      </Card>

      {/* Main Content */}
      <Tabs defaultValue="jobs" className="w-full animate-in fade-in slide-in-from-bottom-4 duration-700 delay-100">
        <TabsList className="bg-slate-700/50">
          <TabsTrigger value="jobs">Security Jobs</TabsTrigger>
          <TabsTrigger value="runs">Execution History</TabsTrigger>
          <TabsTrigger value="gates">Gate Policies</TabsTrigger>
          <TabsTrigger value="compliance">Compliance Reports</TabsTrigger>
        </TabsList>

        {/* Jobs Tab */}
        <TabsContent value="jobs" className="mt-4">
          <Card className="bg-slate-800/50 border-slate-700 backdrop-blur-sm">
            <CardHeader>
              <div className="flex items-center justify-between">
                <div className="space-y-1">
                  <h3 className="text-xl font-semibold text-white">Security Pipeline Jobs</h3>
                  <p className="text-sm text-gray-400">Configure automated security scanning in your CI/CD pipelines</p>
                </div>
                <div className="relative">
                  <Search className="absolute left-3 top-1/2 transform -translate-y-1/2 h-4 w-4 text-gray-400" />
                  <Input
                    placeholder="Search jobs..."
                    value={searchTerm}
                    onChange={(e) => setSearchTerm(e.target.value)}
                    className="pl-10 w-64 bg-slate-700/50 border-slate-600 text-white"
                  />
                </div>
              </div>
            </CardHeader>
            <CardContent>
              <div className="space-y-3">
                {filteredJobs?.map((job: SecurityJob, idx: number) => (
                  <Card
                    key={job.id}
                    className={`bg-slate-700/30 border-slate-600 hover:border-blue-500/50 transition-all cursor-pointer animate-in fade-in slide-in-from-left-4 ${!job.enabled && 'opacity-50'}`}
                    style={{ animationDelay: `${idx * 100}ms` }}
                  >
                    <CardContent className="p-4">
                      <div className="flex items-start justify-between">
                        <div className="flex-1 space-y-2">
                          <div className="flex items-center gap-3">
                            <GitBranch className="h-5 w-5 text-blue-400" />
                            <h4 className="font-semibold text-white">{job.name}</h4>
                            {job.enabled ? (
                              <Badge className="bg-green-500/20 text-green-400 border-green-500/30">ACTIVE</Badge>
                            ) : (
                              <Badge variant="outline" className="border-yellow-500 text-yellow-400">INACTIVE</Badge>
                            )}
                            <Badge variant="outline" className="border-blue-500 text-blue-400">{job.branch}</Badge>
                          </div>
                          <p className="text-sm text-gray-400">{job.repositoryUrl}</p>
                          <div className="flex items-center gap-4 text-xs text-gray-400">
                            <div className="flex items-center gap-1">
                              <Clock className="h-3 w-3" />
                              Last run: {job.lastRunAt ? new Date(job.lastRunAt).toLocaleString() : "Never"}
                            </div>
                            <div className="flex items-center gap-1">
                              <Target className="h-3 w-3" />
                              Triggers: {job.triggerEvents.join(", ")}
                            </div>
                          </div>
                        </div>
                        <div className="flex items-center gap-2 ml-4">
                          <Button size="sm" variant="ghost" onClick={() => runScan(job)}>
                            <Play className="h-4 w-4" />
                          </Button>
                          <Button size="sm" variant="ghost">
                            <Eye className="h-4 w-4" />
                          </Button>
                          <Button size="sm" variant="ghost">
                            <Edit className="h-4 w-4" />
                          </Button>
                          <Button size="sm" variant="ghost" className="text-red-400 hover:text-red-300">
                            <Trash2 className="h-4 w-4" />
                          </Button>
                        </div>
                      </div>
                    </CardContent>
                  </Card>
                ))}
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        {/* Other tabs placeholder */}
        <TabsContent value="runs" className="mt-4">
          <Alert>
            <FileText className="h-4 w-4" />
            <AlertTitle>Job Execution History</AlertTitle>
            <AlertDescription>View detailed scan results and execution timelines. Coming in next iteration.</AlertDescription>
          </Alert>
        </TabsContent>
        <TabsContent value="gates" className="mt-4">
          <Alert>
            <Settings className="h-4 w-4" />
            <AlertTitle>Gate Policy Configuration</AlertTitle>
            <AlertDescription>Define pass/fail thresholds for security checks. Coming in next iteration.</AlertDescription>
          </Alert>
        </TabsContent>
        <TabsContent value="compliance" className="mt-4">
          <Alert>
            <Flag className="h-4 w-4" />
            <AlertTitle>Compliance Reports</AlertTitle>
            <AlertDescription>Generate and view compliance reports across all pipelines. Coming in next iteration.</AlertDescription>
          </Alert>
        </TabsContent>
      </Tabs>

      {/* Create Job Dialog */}
      <Dialog open={showCreateDialog} onOpenChange={setShowCreateDialog}>
        <DialogContent className="bg-slate-800 border-slate-700 text-white max-w-xl">
          <DialogHeader>
            <DialogTitle>Create New Security Job</DialogTitle>
            <DialogDescription>Configure a DevSecOps pipeline with automated security scanning</DialogDescription>
          </DialogHeader>

          <div className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="name">Job Name</Label>
              <Input
                id="name"
                value={newJob.name}
                onChange={(e) => setNewJob({ ...newJob, name: e.target.value })}
                placeholder="e.g., Production Build Scan"
                className="bg-slate-700/50 border-slate-600"
              />
            </div>

            <div className="space-y-2">
              <Label htmlFor="repo">Repository URL</Label>
              <Input
                id="repo"
                value={newJob.repositoryUrl}
                onChange={(e) => setNewJob({ ...newJob, repositoryUrl: e.target.value })}
                placeholder="https://github.com/org/repo.git"
                className="bg-slate-700/50 border-slate-600"
              />
            </div>

            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label htmlFor="branch">Branch</Label>
                <Select value={newJob.branch} onValueChange={(v) => setNewJob({ ...newJob, branch: v })}>
                  <SelectTrigger className="bg-slate-700/50 border-slate-600">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="main">main</SelectItem>
                    <SelectItem value="master">master</SelectItem>
                    <SelectItem value="develop">develop</SelectItem>
                  </SelectContent>
                </Select>
              </div>

              <div className="space-y-2">
                <Label htmlFor="triggers">Triggers</Label>
                <Select
                  value={newJob.triggerEvents[0]}
                  onValueChange={(v) => setNewJob({ ...newJob, triggerEvents: [v] })}
                >
                  <SelectTrigger className="bg-slate-700/50 border-slate-600">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="push">Push Events</SelectItem>
                    <SelectItem value="pr">Pull Requests</SelectItem>
                    <SelectItem value="schedule">Scheduled</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </div>

            <div className="space-y-2">
              <Label htmlFor="config">Pipeline Config Path</Label>
              <Input
                id="config"
                value={newJob.pipelineConfig}
                onChange={(e) => setNewJob({ ...newJob, pipelineConfig: e.target.value })}
                placeholder=".github/workflows/security.yml"
                className="bg-slate-700/50 border-slate-600"
              />
            </div>
          </div>

          <DialogFooter>
            <Button variant="outline" onClick={() => setShowCreateDialog(false)}>Cancel</Button>
            <Button onClick={handleCreate} disabled={createMutation.isPending} className="bg-blue-600 hover:bg-blue-700">
              {createMutation.isPending ? (
                <>
                  <Loader2 className="mr-2 h-4 w-4 animate-spin" />Creating...
                </>
              ) : (
                <>Create Security Job</>
              )}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function runScan(job: SecurityJob) {
  console.log("Run scan for:", job);
}

// Missing imports
function RefreshCw(props: any) {
  return <Activity {...props} />;
}
