import { useQuery } from "@tanstack/react-query";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Shield, AlertTriangle, CheckCircle2, Clock, Target, Database, Eye } from "lucide-react";
import { Link } from "react-router-dom";
import { apiClient } from "@/lib/api";

export function CampaignsPage() {
  const { data: campaignsData, isLoading } = useQuery({
    queryKey: ["engagements_list"],
    queryFn: () => apiClient.getEngagements(),
  });
  
  if (isLoading) {
    return (
      <div className="min-h-screen bg-gradient-to-br from-gray-900 via-slate-900 to-gray-900 flex items-center justify-center">
        <div className="animate-spin rounded-full h-12 w-12 border-t-2 border-b-2 border-red-600" />
      </div>
    );
  }
  
  const campaigns = campaignsData?.engagements || [];
  
  return (
    <div className="min-h-screen bg-gradient-to-br from-gray-900 via-slate-900 to-gray-900">
      {/* Header */}
      <header className="sticky top-0 z-50 glass-effect backdrop-blur-xl border-b border-slate-700/50">
        <div className="container mx-auto px-4 py-4">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-3">
              <Shield className="w-8 h-8 text-red-600" strokeWidth={2} />
              <h1 className="text-2xl font-bold bg-gradient-to-r from-red-500 to-orange-500 bg-clip-text text-transparent">Attack Campaigns</h1>
            </div>
            <Button className="bg-red-600 hover:bg-red-700 text-white">Launch New Campaign →</Button>
          </div>
        </div>
      </header>
      
      <main className="container mx-auto px-4 py-8">
        {/* Stats Overview */}
        <div className="grid md:grid-cols-3 gap-6 mb-8">
          <Card className="glass-effect backdrop-blur-lg border-slate-700/50">
            <CardContent className="pt-6">
              <div className="flex items-center justify-between">
                <div>
                  <p className="text-sm text-gray-400">Active Attacks</p>
                  <p className="text-3xl font-bold text-green-400 mt-1">{campaigns.filter(e => e.status === 'active').length}</p>
                </div>
                <CheckCircle2 className="w-8 h-8 text-green-500" />
              </div>
            </CardContent>
          </Card>
          
          <Card className="glass-effect backdrop-blur-lg border-slate-700/50">
            <CardContent className="pt-6">
              <div className="flex items-center justify-between">
                <div>
                  <p className="text-sm text-gray-400">Completed</p>
                  <p className="text-3xl font-bold text-blue-400 mt-1">{campaigns.filter(e => e.status === 'completed').length}</p>
                </div>
                <Clock className="w-8 h-8 text-blue-500" />
              </div>
            </CardContent>
          </Card>
          
          <Card className="glass-effect backdrop-blur-lg border-slate-700/50">
            <CardContent className="pt-6">
              <div className="flex items-center justify-between">
                <div>
                  <p className="text-sm text-gray-400">Total Scans</p>
                  <p className="text-3xl font-bold text-white mt-1">{campaigns.length}</p>
                </div>
                <Database className="w-8 h-8 text-white" />
              </div>
            </CardContent>
          </Card>
        </div>
        
        {/* Campaign List */}
        <Tabs defaultValue="all" className="space-y-6">
          <TabsList className="grid w-full max-w-md grid-cols-4 bg-slate-800/50">
            <TabsTrigger value="all" className="data-[state=active]:bg-red-600 data-[state=active]:text-white">All</TabsTrigger>
            <TabsTrigger value="active" className="data-[state=active]:bg-red-600 data-[state=active]:text-white">Active</TabsTrigger>
            <TabsTrigger value="pending" className="data-[state=active]:bg-red-600 data-[state=active]:text-white">Pending</TabsTrigger>
            <TabsTrigger value="completed" className="data-[state=active]:bg-red-600 data-[state=active]:text-white">Done</TabsTrigger>
          </TabsList>
          
          <TabsContent value="all">
            {campaigns.length ? (
              <div className="space-y-4">
                {campaigns.map((campaign) => (
                  <Card key={campaign.id} className="glass-effect backdrop-blur-lg border-slate-700/50">
                    <CardContent className="pt-6">
                      <div className="flex items-center justify-between">
                        <div className="flex items-start gap-4 flex-1">
                          <div className={`w-12 h-12 rounded-lg flex items-center justify-center ${
                            campaign.status === 'active' ? 'bg-green-500/10 text-green-400' :
                            campaign.status === 'pending' ? 'bg-yellow-500/10 text-yellow-400' :
                            campaign.status === 'completed' ? 'bg-blue-500/10 text-blue-400' :
                            'bg-gray-500/10 text-gray-400'
                          }`}>
                            {campaign.status === 'active' && <CheckCircle2 className="w-6 h-6" />}
                            {campaign.status === 'pending' && <Clock className="w-6 h-6" />}
                            {campaign.status === 'completed' && <Shield className="w-6 h-6" />}
                          </div>
                          
                          <div className="flex-1 space-y-2">
                            <Target className="w-4 h-4 text-gray-500" />
                            <p className="text-lg font-semibold text-white">{campaign.scope.targets.join(", ")}</p>
                            
                            <div className="flex items-center gap-4 text-sm text-gray-400">
                              <span>{new Date(campaign.created_at).toLocaleDateString()}</span>
                              <span>•</span>
                              <span className="capitalize">{campaign.status}</span>
                            </div>
                          </div>
                        </div>
                        
                        <Link to={`/engagements/${campaign.id}`}>
                          <Button variant="outline" size="sm" className="border-slate-700 hover:bg-slate-800 text-gray-300">
                            <Eye className="w-4 h-4 mr-2" />
                            View Details
                          </Button>
                        </Link>
                      </div>
                    </CardContent>
                  </Card>
                ))}
              </div>
            ) : (
              <Card className="glass-effect backdrop-blur-lg border-slate-700/50">
                <CardContent className="pt-20 text-center">
                  <Shield className="w-16 h-16 text-gray-600 mx-auto mb-4" />
                  <h3 className="text-xl font-semibold text-white mb-2">No Campaigns Found</h3>
                  <p className="text-gray-400 mb-6">Start your first penetration testing campaign today!</p>
                  <Link to="/quickscan">
                    <Button className="bg-red-600 hover:bg-red-700 text-white">Launch Quick Scan →</Button>
                  </Link>
                </CardContent>
              </Card>
            )}
          </TabsContent>
          
          <TabsContent value="active"></TabsContent>
          <TabsContent value="pending"></TabsContent>
          <TabsContent value="completed"></TabsContent>
        </Tabs>
      </main>
    </div>
  );
}
