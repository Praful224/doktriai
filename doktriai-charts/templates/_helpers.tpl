{{/*
Expand the name of the chart.
*/}}
{{- define "doktriai.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "doktriai.fullname" -}}
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
Common labels
*/}}
{{- define "doktriai.labels" -}}
helm.sh/chart: {{ include "doktriai.name" . }}-{{ .Chart.Version | replace "+" "_" }}
{{ include "doktriai.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
io.doktri.managed: "true"
{{- end }}

{{/*
Selector labels
*/}}
{{- define "doktriai.selectorLabels" -}}
app.kubernetes.io/name: {{ include "doktriai.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}
