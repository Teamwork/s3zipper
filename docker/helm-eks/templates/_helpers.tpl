{{/*
Expand the name of the chart.
*/}}
{{- define "eks.name" -}}
{{- default .Chart.Name .name | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "eks.fullname" -}}
{{- $name := default .Chart.Name .name }}
{{- if contains .Release.Name $name }}
{{- $name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "eks.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "eks.labels" -}}
helm.sh/chart: {{ include "eks.chart" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "eks.selectorLabels" -}}
app.kubernetes.io/name: {{ include "eks.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create ALB ingress group name
*/}}
{{- define "eks.albGroupName" -}}
{{- $base := "ingress" }}
{{- if not (empty .group) }}
{{- $base = printf "ingress-%s" .group }}
{{- end }}
{{- if .internal }}
{{- printf "%s-internal" $base }}
{{- else }}
{{- printf "%s-external" $base }}
{{- end }}
{{- end }}

{{/*
DataDog labels
*/}}
{{- define "eks.datadogLabels" -}}
tags.datadoghq.com/env: {{ .env | default "prod" }}
tags.datadoghq.com/service: {{ .name }}
tags.datadoghq.com/version: {{ .Chart.AppVersion | quote }}
{{- end }}
