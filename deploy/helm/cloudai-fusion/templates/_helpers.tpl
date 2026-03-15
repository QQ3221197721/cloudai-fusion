{{/*
=======================================================================
CloudAI Fusion — Helm Template Helpers
Standard naming, labeling, and utility functions
=======================================================================
*/}}

{{/*
Expand the name of the chart.
*/}}
{{- define "cloudai-fusion.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited
to this (by the DNS naming spec). If release name contains chart name
it will be used as a full name.
*/}}
{{- define "cloudai-fusion.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "cloudai-fusion.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Namespace — always use the release namespace.
*/}}
{{- define "cloudai-fusion.namespace" -}}
{{- .Release.Namespace }}
{{- end }}

{{/*
Common labels (Kubernetes recommended labels).
*/}}
{{- define "cloudai-fusion.labels" -}}
helm.sh/chart: {{ include "cloudai-fusion.chart" . }}
{{ include "cloudai-fusion.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: cloudai-fusion
{{- end }}

{{/*
Selector labels — used in both Deployment.spec.selector and Service.spec.selector.
*/}}
{{- define "cloudai-fusion.selectorLabels" -}}
app.kubernetes.io/name: {{ include "cloudai-fusion.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Component labels — extends common labels with a component identifier.
Usage: {{ include "cloudai-fusion.componentLabels" (dict "context" . "component" "apiserver") }}
*/}}
{{- define "cloudai-fusion.componentLabels" -}}
{{ include "cloudai-fusion.labels" .context }}
app.kubernetes.io/component: {{ .component }}
{{- end }}

{{/*
Component selector labels — extends selector labels with a component identifier.
Usage: {{ include "cloudai-fusion.componentSelectorLabels" (dict "context" . "component" "apiserver") }}
*/}}
{{- define "cloudai-fusion.componentSelectorLabels" -}}
{{ include "cloudai-fusion.selectorLabels" .context }}
app.kubernetes.io/component: {{ .component }}
{{- end }}

{{/*
Service account name for a given component.
Usage: {{ include "cloudai-fusion.serviceAccountName" (dict "context" . "component" "apiserver") }}
*/}}
{{- define "cloudai-fusion.serviceAccountName" -}}
{{- if .context.Values.serviceAccount.create }}
{{- default (printf "%s-%s" (include "cloudai-fusion.fullname" .context) .component) .context.Values.serviceAccount.name }}
{{- else }}
{{- default "default" .context.Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Create the image reference for a component.
Usage: {{ include "cloudai-fusion.image" (dict "context" . "image" .Values.apiserver.image) }}
*/}}
{{- define "cloudai-fusion.image" -}}
{{- $registry := .image.registry | default .context.Values.global.imageRegistry -}}
{{- $repository := .image.repository -}}
{{- $tag := .image.tag | default .context.Chart.AppVersion -}}
{{- if $registry }}
{{- printf "%s/%s:%s" $registry $repository $tag }}
{{- else }}
{{- printf "%s:%s" $repository $tag }}
{{- end }}
{{- end }}

{{/*
Image pull policy.
*/}}
{{- define "cloudai-fusion.imagePullPolicy" -}}
{{- .Values.global.imagePullPolicy | default "IfNotPresent" }}
{{- end }}

{{/*
Pod security context (shared across all workloads).
*/}}
{{- define "cloudai-fusion.podSecurityContext" -}}
runAsNonRoot: {{ .Values.securityContext.runAsNonRoot | default true }}
runAsUser: {{ .Values.securityContext.runAsUser | default 1000 }}
fsGroup: {{ .Values.securityContext.fsGroup | default 1000 }}
seccompProfile:
  type: RuntimeDefault
{{- end }}

{{/*
Container security context (hardened defaults).
*/}}
{{- define "cloudai-fusion.containerSecurityContext" -}}
allowPrivilegeEscalation: false
readOnlyRootFilesystem: true
capabilities:
  drop:
    - ALL
{{- end }}

{{/*
Annotations for Prometheus scraping.
Usage: {{ include "cloudai-fusion.prometheusAnnotations" (dict "port" "9100") }}
*/}}
{{- define "cloudai-fusion.prometheusAnnotations" -}}
prometheus.io/scrape: "true"
prometheus.io/port: {{ .port | quote }}
prometheus.io/path: "/metrics"
{{- end }}

{{/*
PostgreSQL host — resolves to the in-cluster service or external.
*/}}
{{- define "cloudai-fusion.postgresql.host" -}}
{{- if .Values.postgresql.enabled }}
{{- printf "%s-postgresql" (include "cloudai-fusion.fullname" .) }}
{{- else }}
{{- .Values.database.host }}
{{- end }}
{{- end }}

{{/*
Redis host — resolves to the in-cluster service or external.
*/}}
{{- define "cloudai-fusion.redis.host" -}}
{{- if .Values.redis.enabled }}
{{- printf "%s-redis-master:6379" (include "cloudai-fusion.fullname" .) }}
{{- else }}
{{- printf "%s:%s" (.Values.redis.host | default "redis") (.Values.redis.port | default "6379") }}
{{- end }}
{{- end }}

{{/*
Checksum annotation for config/secrets — triggers rolling restart on change.
Usage: {{ include "cloudai-fusion.checksumAnnotation" . }}
*/}}
{{- define "cloudai-fusion.checksumAnnotation" -}}
checksum/config: {{ include (print $.Template.BasePath "/secrets.yaml") . | sha256sum }}
{{- end }}

{{/*
Standard pod topology spread constraints for HA.
Usage: {{ include "cloudai-fusion.topologySpreadConstraints" (dict "component" "apiserver") | nindent 8 }}
*/}}
{{- define "cloudai-fusion.topologySpreadConstraints" -}}
- maxSkew: 1
  topologyKey: kubernetes.io/hostname
  whenUnsatisfiable: DoNotSchedule
  labelSelector:
    matchLabels:
      app.kubernetes.io/component: {{ .component }}
{{- end }}
