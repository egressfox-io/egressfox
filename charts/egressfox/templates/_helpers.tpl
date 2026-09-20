{{- define "egressfox.name" -}}
{{- printf "%s-egressfox" .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
