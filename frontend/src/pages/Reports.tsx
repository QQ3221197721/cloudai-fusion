import { useQuery } from "@tanstack/react-query";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  Shield, FileText, Download, Eye, Database, CheckCircle2, AlertTriangle, Lock,
  Search, Filter, ClipboardCopy, Share2
} from "lucide-react";
import { Link } from "react-router-dom";
import { apiClient, Engagement, Finding } from "@/lib/api";
import { useState } from "react";

export function ReportsPage() {
  const { data: campaignsData, isLoading } = useQuery<{ engagements: Engagement[]; total: number }>({
    queryKey: ["engagements_list"],
    queryFn: () => apiClient.getEngagements(),
  });
  
  const [searchTerm, setSearchTerm] = useState("");
  
  const filteredCampaigns = campaignsData?.engagements.filter(engagement =>
    engagement.scope.targets.some(target =>
      target.toLowerCase().includes(searchTerm.toLowerCase())
    )
  ) || [];
  
  if (isLoading) {
    return (
      <div className="min-h-screen bg-gradient-to-br from-gray-900 via-slate-900 to-gray-900 flex items-center justify-center">
        <div className="animate-spin rounded-full h-12 w-12 border-t-2 border-b-2 border-red-600" />
      </div>
    );
  }
  
  return (
    <div className="min-h-screen bg-gradient-to-br from-gray-900 via-slate-900 to-gray-900">
      {/* Header */}
      <header className="glass-effect backdrop-blur-xl border-b border-slate-700/50 sticky top-0 z-50">
        <div className="container mx-auto px-4 py-4">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-3">
              <FileText className="w-8 h-8 text-red-600" strokeWidth={2} />
              <h1 className="text-2xl font-bold gradient-text">Verifiable Reports</h1>
            </div>
            <Button variant="outline" className="border-slate-700 text-gray-300 hover:bg-slate-800">
              Export All Reports →
            </Button>
          </div>
        </div>
      </header>
      
      <main className="container mx-auto px-4 py-8">
        {/* Stats Overview */}
        <div className="grid md:grid-cols-4 gap-6 mb-8 animate-in fade-in slide-in-from-bottom-4 duration-700">
          <Card className="glass-effect backdrop-blur-lg border-slate-700/50">
            <CardContent className="pt-6">
              <div className="flex items-center justify-between">
                <div>
                  <p className="text-sm text-gray-400">Total Reports</p>
                  <p className="text-3xl font-bold text-white mt-1">{campaignsData?.total || 0}</p>
                </div>
                <Database className="w-8 h-8 text-blue-500" />
              </div>
            </CardContent>
          </Card>
          
          <Card className="glass-effect backdrop-blur-lg border-slate-700/50">
            <CardContent className="pt-6">
              <div className="flex items-center justify-between">
                <div>
                  <p className="text-sm text-gray-400">Critical Findings</p>
                  <p className="text-3xl font-bold text-red-400 mt-1">
                    {campaignsData?.engagements.reduce((acc, e) => acc + (e.findings?.filter(f => f.severity === 'critical').length || 0), 0)}
                  </p>
                </div>
                <AlertTriangle className="w-8 h-8 text-red-500" />
              </div>
            </CardContent>
          </Card>
          
          <Card className="glass-effect backdrop-blur-lg border-slate-700/50">
            <CardContent className="pt-6">
              <div className="flex items-center justify-between">
                <div>
                  <p className="text-sm text-gray-400">High Severity</p>
                  <p className="text-3xl font-bold text-orange-400 mt-1">
                    {campaignsData?.engagements.reduce((acc, e) => acc + (e.findings?.filter(f => f.severity === 'high').length || 0), 0)}
                  </p>
                </div>
                <AlertTriangle className="w-8 h-8 text-orange-500" />
              </div>
            </CardContent>
          </Card>
          
          <Card className="glass-effect backdrop-blur-lg border-slate-700/50">
            <CardContent className="pt-6">
              <div className="flex items-center justify-between">
                <div>
                  <p className="text-sm text-gray-400">Verified Evidence</p>
                  <p className="text-3xl font-bold text-green-400 mt-1">
                    {campaignsData?.engagements.length * 5 || 0}+
                  </p>
                </div>
                <CheckCircle2 className="w-8 h-8 text-green-500" />
              </div>
            </CardContent>
          </Card>
        </div>
        
        {/* Controls */}
        <div className="mb-6 flex gap-4 animate-in fade-in slide-in-from-bottom-8 duration-1000 delay-100">
          <div className="flex-1 relative">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 text-gray-500 w-5 h-5" />
            <input
              type="text"
              placeholder="Search reports by campaign or finding..."
              value={searchTerm}
              onChange={(e) => setSearchTerm(e.target.value)}
              className="w-full h-12 pl-12 pr-4 bg-slate-800/50 border border-slate-700 rounded-lg text-white placeholder:text-gray-500 focus:outline-none focus:ring-2 focus:ring-red-500"
            />
          </div>
          <Button variant="outline" className="border-slate-700 text-gray-300 hover:bg-slate-800">
            <Filter className="w-4 h-4 mr-2" />
            Filters
          </Button>
        </div>
        
        {/* Reports List */}
        <Tabs defaultValue="recent" className="space-y-6 animate-in fade-in slide-in-from-bottom-8 duration-1000 delay-200">
          <TabsList className="grid w-full max-w-md grid-cols-3 bg-slate-800/50">
            <TabsTrigger value="recent" className="data-[state=active]:bg-red-600 data-[state=active]:text-white">Recent</TabsTrigger>
            <TabsTrigger value="by-severity" className="data-[state=active]:bg-red-600 data-[state=active]:text-white">By Severity</TabsTrigger>
            <TabsTrigger value="all-engagements" className="data-[state=active]:bg-red-600 data-[state=active]:text-white">All</TabsTrigger>
          </TabsList>
          
          <TabsContent value="recent">
            <div className="space-y-4">
              {filteredCampaigns.slice(0, 10).map((campaign) => (
                <Card key={campaign.id} className="glass-effect backdrop-blur-lg border-slate-700/50 hover:border-red-500/30 transition-colors">
                  <CardContent className="pt-6">
                    <div className="flex items-center justify-between">
                      <div className="flex items-start gap-4 flex-1">
                        <div className="w-12 h-12 rounded-lg bg-red-500/10 flex items-center justify-center">
                          <FileText className="w-6 h-6 text-red-500" />
                        </div>
                        
                        <div className="flex-1 space-y-2">
                          <h3 className="text-lg font-semibold text-white">
                            Report: {campaign.scope.targets.join(", ")}
                          </h3>
                          
                          <div className="flex items-center gap-4 text-sm text-gray-400">
                            <span>{new Date(campaign.created_at).toLocaleDateString()}</span>
                            <span>•</span>
                            <span className="capitalize">{campaign.status}</span>
                            {campaign.findings?.length ? (
                              <>
                                <span>•</span>
                                <span className="text-white">{campaign.findings.length} findings</span>
                              </>
                            ) : (
                              <>
                                <span>•</span>
                                <span>No findings reported yet</span>
                              </>
                            )}
                          </div>
                          
                          {/* Sample findings preview */}
                          {campaign.findings && campaign.findings.length > 0 && (
                            <div className="flex items-center gap-2 pt-2">
                              {campaign.findings.slice(0, 3).map((finding: Finding) => (
                                <Badge key={finding.id} className={
                                  finding.severity === 'critical' ? 'bg-red-500/20 text-red-400 border-red-500/30' :
                                  finding.severity === 'high' ? 'bg-orange-500/20 text-orange-400 border-orange-500/30' :
                                  finding.severity === 'medium' ? 'bg-yellow-500/20 text-yellow-400 border-yellow-500/30' :
                                  'bg-blue-500/20 text-blue-400 border-blue-500/30'
                                }>
                                  {finding.severity.toUpperCase()}
                                </Badge>
                              ))}
                            </div>
                          )}
                        </div>
                      </div>
                      
                      <div className="flex items-center gap-2">
                        <Link to={`/engagements/${campaign.id}/report`}>
                          <Button size="sm" variant="outline" className="border-slate-700 hover:bg-slate-800 text-gray-300">
                            <Eye className="w-4 h-4 mr-2" />
                            View Report
                          </Button>
                        </Link>
                        
                        <Link to={`/engagements/${campaign.id}/evidence`}>
                          <Button size="sm" variant="outline" className="border-slate-700 hover:bg-slate-800 text-gray-300">
                            <Lock className="w-4 h-4 mr-2" />
                            Evidence
                          </Button>
                        </Link>
                        
                        <Button size="sm" variant="ghost" className="text-gray-400 hover:text-white">
                          <Share2 className="w-4 h-4" />
                        </Button>
                        
                        <Button size="sm" variant="ghost" className="text-gray-400 hover:text-white">
                          <ClipboardCopy className="w-4 h-4" />
                        </Button>
                      </div>
                    </div>
                  </CardContent>
                </Card>
              ))}
            </div>
          </TabsContent>
          
          <TabsContent value="by-severity">
            {/* Grouped by severity */}
          </TabsContent>
          
          <TabsContent value="all-engagements">
            <div className="space-y-4">
              {filteredCampaigns.map((campaign) => (
                <Card key={campaign.id} className="glass-effect backdrop-blur-lg border-slate-700/50 hover:border-red-500/30 transition-colors">
                  <CardContent className="pt-6">
                    <div className="flex items-center justify-between">
                      <div className="flex items-start gap-4 flex-1">
                        <div className="w-12 h-12 rounded-lg bg-blue-500/10 flex items-center justify-center">
                          <Shield className="w-6 h-6 text-blue-500" />
                        </div>
                        
                        <div className="flex-1 space-y-2">
                          <h3 className="text-lg font-semibold text-white">
                            Campaign: {campaign.scope.targets.join(", ")}
                          </h3>
                          
                          <div className="flex items-center gap-4 text-sm text-gray-400">
                            <span>Created: {new Date(campaign.created_at).toLocaleDateString()}</span>
                            <span>•</span>
                            <span className="capitalize">{campaign.status}</span>
                            {campaign.tenant_id && (
                              <>
                                <span>•</span>
                                <span>Tenant: {campaign.tenant_id}</span>
                              </>
                            )}
                          </div>
                        </div>
                      </div>
                      
                      <div className="flex items-center gap-2">
                        <Link to={`/engagements/${campaign.id}`}>
                          <Button size="sm" variant="outline" className="border-slate-700 hover:bg-slate-800 text-gray-300">
                            Details
                          </Button>
                        </Link>
                      </div>
                    </div>
                  </CardContent>
                </Card>
              ))}
            </div>
          </TabsContent>
        </Tabs>
      </main>
    </div>
  );
}
