{{- define "addon-webhook.fullname" -}}
{{- $name := printf "%s-%s" .Release.Name .Chart.Name -}}
{{- if gt (len $name) 54 -}}
{{- printf "%s-%s" ($name | trunc 45 | trimSuffix "-") ($name | sha256sum | trunc 8) -}}
{{- else -}}
{{- $name -}}
{{- end -}}
{{- end -}}

{{- define "addon-webhook.labels" -}}
app.kubernetes.io/name: {{ .Chart.Name }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: admission-webhook
{{- end -}}

{{- define "addon-webhook.tls.setup" -}}
{{- if not (hasKey . "addonCA") -}}
  {{- $name := include "addon-webhook.fullname" . -}}
  {{- $ca := genCA (printf "%s CA" $name) (int .Values.tls.caValidityDays) -}}
  {{- with lookup "v1" "Secret" .Release.Namespace (printf "%s-ca" $name) -}}
    {{- if and (index .data "ca.crt") (index .data "ca.key") -}}
      {{- $ca = buildCustomCert (index .data "ca.crt") (index .data "ca.key") -}}
    {{- end -}}
  {{- end -}}
  {{- $_ := set . "addonCA" $ca -}}
  {{- $dns := list $name (printf "%s.%s" $name .Release.Namespace) (printf "%s.%s.svc" $name .Release.Namespace) (printf "%s.%s.svc.cluster.local" $name .Release.Namespace) -}}
  {{- $cert := genSignedCert (printf "%s.%s.svc" $name .Release.Namespace) nil $dns (int .Values.tls.certValidityDays) $ca -}}
  {{- $crt := $cert.Cert | b64enc -}}
  {{- $key := $cert.Key | b64enc -}}
  {{- if not (and (eq .Values.tls.method "helm") .Values.tls.helm.renewOnUpgrade) -}}
    {{- with lookup "v1" "Secret" .Release.Namespace (printf "%s-tls" $name) -}}
      {{- if and (index .data "tls.crt") (index .data "tls.key") -}}
        {{- $crt = index .data "tls.crt" -}}
        {{- $key = index .data "tls.key" -}}
      {{- end -}}
    {{- end -}}
  {{- end -}}
  {{- $_ := set . "addonTLSCrt" $crt -}}
  {{- $_ := set . "addonTLSKey" $key -}}
{{- end -}}
{{- end -}}
