{{- define "tenantry.labels" -}}
app.kubernetes.io/part-of: tenantry
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version }}
{{- end }}

{{- define "tenantry.image" -}}
{{ .Values.image.repository }}:{{ .Values.image.tag | default .Chart.AppVersion }}
{{- end }}

{{/* Pod-level security that satisfies the "restricted" Pod Security Standard. */}}
{{- define "tenantry.podSecurity" -}}
runAsNonRoot: true
runAsUser: 65532
runAsGroup: 65532
seccompProfile:
  type: RuntimeDefault
{{- end }}

{{- define "tenantry.containerSecurity" -}}
allowPrivilegeEscalation: false
readOnlyRootFilesystem: true
capabilities:
  drop: [ALL]
{{- end }}

{{/* Environment shared by every binary; see backend/internal/config. */}}
{{- define "tenantry.env" -}}
- name: DATABASE_URL
  valueFrom:
    secretKeyRef:
      name: {{ required "database.existingSecret is required" .Values.database.existingSecret }}
      key: {{ .Values.database.key }}
- name: TENANTRY_LOG_LEVEL
  value: {{ .Values.log.level | quote }}
- name: TENANTRY_LOG_FORMAT
  value: json
{{- end }}

{{/* Pod placement shared by every Pod of the chart. */}}
{{- define "tenantry.scheduling" -}}
{{- with .Values.nodeSelector }}
nodeSelector:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- with .Values.tolerations }}
tolerations:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- end }}
