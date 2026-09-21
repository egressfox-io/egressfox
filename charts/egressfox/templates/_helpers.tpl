{{- define "egressfox.name" -}}
{{- printf "%s-egressfox" .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- define "egressfox.image" -}}
{{- if .Values.image.digest -}}
{{- printf "%s@%s" .Values.image.repository .Values.image.digest -}}
{{- else -}}
{{- printf "%s:%s" .Values.image.repository (default .Chart.AppVersion .Values.image.tag) -}}
{{- end -}}
{{- end -}}
