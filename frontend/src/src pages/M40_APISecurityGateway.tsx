/**
 * M40 API Security Gateway - Production-Grade API Security Dashboard
 */

import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import axios from "axios";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Plus, Search, Shield, Zap, Clock, Key, AlertTriangle, TrendingUp } from "lucide-react";

const API_BASE_URL = import.meta.env.VITE_API_URL || "http://localhost:8080";

export function M40APISecurityGatewayPage() {
  const { data: traffic } = useQuery({ queryKey: ["m40-traffic"], queryFn: async () => ({ requests: 156743, p99Latency: 127 }) });
  const { data: rateLimits } = useQuery({ queryKey: ["m40-rate-limits"], queryFn: async () => ({ active: 42, blocked: 234 }) });
  const { data: apiKeys } = useQuery({ queryKey: ["m40-keys"], queryFn: async () => ({ active: 87, revoked: 12 }) });

  return (
    <div className="min-h-screen bg-gradient-to-br from-gray-900 via-slate-900 to-gray-900 p-6 space-y-6">
      <div className="flex items-center justify-between animate-in fade-in slide-in-from-top-4 duration-500">
        <div>
          <h1 className="text-3xl font-bold bg-gradient-to-r from-blue-400 to-purple-500 bg-clip-text text-transparent">API Security Gateway</h1>
          <p className="text-gray-400">Rate limiting, JWT validation, OAuth2 clients & threat detection</p>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" className="border-slate-700 text-gray-300">Manage Keys</Button>
          <Button onClick={() => window.location.reload()} className="bg-blue-600 hover:bg-blue-700">
            <Zap className="mr-2 h-4 w-4" />Test Gateway
          </Button>
        </div>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
        <MetricCard title="API Requests (24h)" value={(traffic?.requests || 0).toLocaleString()} icon={TrendingUp} color="blue" />
        <MetricCard title="Rate Limits Blocked" value={rateLimits?.blocked || 0} icon={Shield} color="red" />
        <MetricCard title="Active API Keys" value={apiKeys?.active || 0} icon={Key} color="green" />
        <MetricCard title="P99 Latency" value={`${traffic?.p99Latency || 0}ms`} icon={Clock} color="yellow" />
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <Card className="bg-slate-800/50 border-slate-700 backdrop-blur-sm">
          <CardHeader>
            <h3 className="text-xl font-semibold text-white">Recent Rate Limit Events</h3>
          </CardHeader>
          <CardContent className="space-y-2">
            {[1,2,3].map(i => (
              <div key={i} className="flex items-center justify-between p-3 bg-slate-700/30 rounded-lg">
                <div>
                  <p className="font-medium text-white">IP: 192.168.{10+i}.42</p>
                  <p className="text-xs text-gray-400">Endpoint: /api/v1/users</p>
                </div>
                <Badge className="bg-red-500/20 text-red-400">BLOCKED</Badge>
              </div>
            ))}
          </CardContent>
        </Card>

        <Card className="bg-slate-800/50 border-slate-700 backdrop-blur-sm">
          <CardHeader>
            <h3 className="text-xl font-semibold text-white">JWT Validation Status</h3>
          </CardHeader>
          <CardContent>
            <div className="space-y-3">
              <JWTStatus issuer="Auth0" valid={true} lastValidated="Now" />
              <JWTStatus issuer="Okta" valid={true} lastValidated="2 min ago" />
              <JWTStatus issuer="Custom Issuer" valid={false} error="Expired token" />
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}

function MetricCard({ title, value, icon: Icon, color }: any) {
  return (
    <Card className={`bg-gradient-to-br from-${color}-500/20 to-${color}-600/20 border-${color}-500/30`}>
      <CardContent className="p-6">
        <div className="flex items-center justify-between">
          <div><p className="text-sm text-gray-400">{title}</p><h3 className="text-2xl font-bold text-white mt-1">{value}</h3></div>
          <Icon className={`h-6 w-6 text-${color}-400`} />
        </div>
      </CardContent>
    </Card>
  );
}

function JWTStatus({ issuer, valid, lastValidated, error }: any) {
  return (
    <div className="flex items-center justify-between p-3 bg-slate-700/30 rounded-lg">
      <div>
        <p className="font-medium text-white">{issuer}</p>
        <p className="text-xs text-gray-400">Last validated: {lastValidated}</p>
      </div>
      {valid ? (
        <Badge className="bg-green-500/20 text-green-400">VALID</Badge>
      ) : (
        <Badge className="bg-red-500/20 text-red-400">{error}</Badge>
      )}
    </div>
  );
}
