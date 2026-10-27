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

{{/*
An environment variable read from a key of an existing Secret, so the
credential never passes through values. Call with
(dict "name" "DATABASE_URL" "ref" .Values.postgres.urlSecret "path" "postgres.urlSecret");
path is the ref's place in values, for the error naming it.
*/}}
{{- define "openplan.secretEnv" -}}
- name: {{ .name }}
  valueFrom:
    secretKeyRef:
      name: {{ required (printf "%s.name is required: the Secret holding %s" .path .name) .ref.name }}
      key: {{ required (printf "%s.key is required: the key in that Secret holding %s" .path .name) .ref.key }}
{{- end -}}

{{/*
Temporal, which the api and the executor reach alike. Call with $.
*/}}
{{- define "openplan.temporalEnv" -}}
- name: TEMPORAL_ADDRESS
  value: {{ required "temporal.address is required: the Temporal frontend, host:port" .Values.temporal.address | quote }}
- name: TEMPORAL_NAMESPACE
  value: {{ .Values.temporal.namespace | quote }}
{{- end -}}

{{/*
The artifact store, which the api and the executor share: the executor writes
run logs and plans, the api reads them. Always S3, since the two pods share
no volume. Call with $.
*/}}
{{- define "openplan.s3Env" -}}
- name: ARTIFACT_STORE_KIND
  value: s3
- name: S3_BUCKET
  value: {{ required "s3.bucket is required: the bucket for run logs and plans" .Values.s3.bucket | quote }}
- name: S3_REGION
  value: {{ required "s3.region is required: the bucket's region" .Values.s3.region | quote }}
{{- with .Values.s3.endpoint }}
- name: S3_ENDPOINT
  value: {{ . | quote }}
{{- end }}
- name: S3_FORCE_PATH_STYLE
  value: {{ .Values.s3.forcePathStyle | quote }}
{{ include "openplan.secretEnv" (dict "name" "S3_ACCESS_KEY_ID" "ref" .Values.s3.accessKeyIDSecret "path" "s3.accessKeyIDSecret") }}
{{ include "openplan.secretEnv" (dict "name" "S3_SECRET_ACCESS_KEY" "ref" .Values.s3.secretAccessKeySecret "path" "s3.secretAccessKeySecret") }}
{{- end -}}
