{{/*
The release name when it already says openplan, else <release>-openplan.
Truncated to 54 so the longest component name, <fullname>-executor, still
fits the 63-character limit on Kubernetes names.
*/}}
{{- define "openplan.fullname" -}}
{{- if contains "openplan" .Release.Name -}}
{{- .Release.Name | trunc 54 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-openplan" .Release.Name | trunc 54 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{/*
Selector labels for one component. Deployments can't change their selector
after creation, so this set never grows; other labels go in openplan.labels.
Call with (dict "context" $ "component" "api").
*/}}
{{- define "openplan.selectorLabels" -}}
app.kubernetes.io/name: openplan
app.kubernetes.io/instance: {{ .context.Release.Name }}
app.kubernetes.io/component: {{ .component }}
{{- end -}}

{{/*
All labels for one component's objects. Call like openplan.selectorLabels.
*/}}
{{- define "openplan.labels" -}}
{{ include "openplan.selectorLabels" . }}
helm.sh/chart: {{ printf "%s-%s" .context.Chart.Name .context.Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
app.kubernetes.io/version: {{ .context.Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .context.Release.Service }}
{{- end -}}

{{/*
The image of one component: <registry>/openplan-<component>:<tag>.
Call with (dict "context" $ "component" "api").
*/}}
{{- define "openplan.image" -}}
{{- $image := .context.Values.image -}}
{{- printf "%s/openplan-%s:%s" $image.registry .component ($image.tag | default .context.Chart.AppVersion) -}}
{{- end -}}
