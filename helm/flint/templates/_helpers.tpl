{{/* Chart name */}}
{{- define "flint.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/* Fully qualified app name */}}
{{- define "flint.fullname" -}}
{{- printf "%s" .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/* Common labels */}}
{{- define "flint.labels" -}}
app.kubernetes.io/name: {{ include "flint.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{/* Selector labels */}}
{{- define "flint.selectorLabels" -}}
app.kubernetes.io/name: {{ include "flint.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/* Service account name */}}
{{- define "flint.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "flint.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{/* Server image */}}
{{- define "flint.serverImage" -}}
{{- printf "%s:%s" .Values.image.repository (default .Chart.AppVersion .Values.image.tag) -}}
{{- end -}}

{{/* Database host: internal postgres service or external */}}
{{- define "flint.dbHost" -}}
{{- if .Values.postgresql.internal -}}
{{- printf "%s-postgres" (include "flint.fullname" .) -}}
{{- else -}}
{{- required "database.host is required (or set postgresql.internal=true for evaluation)" .Values.database.host -}}
{{- end -}}
{{- end -}}

{{/* Core secrets: generate-once-and-preserve. On first install, empty values
     are filled with random material; upgrades reuse the existing Secret so
     sessions and encrypted data survive. */}}
{{- define "flint.coreSecretName" -}}
{{- if .Values.secrets.existingSecret -}}
{{- .Values.secrets.existingSecret -}}
{{- else -}}
{{- printf "%s-core" (include "flint.fullname" .) -}}
{{- end -}}
{{- end -}}
