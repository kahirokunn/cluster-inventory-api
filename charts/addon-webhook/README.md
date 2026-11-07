# Addon admission webhook

This chart serves admission checks for the `multicluster.x-k8s.io/v1alpha1`
Addon and AddonClass APIs. It includes the two CRDs, a webhook Deployment and
Service, and two validating webhook rules:

| Request | Decision | API access needed |
| --- | --- | --- |
| Update an Addon's `spec.classRef` | Permit a rebase when the old and new Classes have the same `controllerName`; if a manager is recorded on the Addon, it must match | Get AddonClasses in any namespace |
| Delete an AddonClass | Reject while an Addon's `status.classRef` records its namespace, name, and `controllerName`, including Addons being deleted | List Addons across namespaces |

Both rules use `failurePolicy: Fail`. Addon updates and AddonClass deletions are
rejected while the server cannot answer. Addon status updates do not invoke the
UPDATE rule. The selected addon manager remains responsible for observing
Classes, reporting status, and cleaning up installations.

## TLS configuration

| `tls.method` | Certificate source | Renewal and CA bundle |
| --- | --- | --- |
| `helm` (default) | Helm generates a CA and Service certificate | Existing Secrets are reused on upgrade. `tls.helm.renewOnUpgrade=true` replaces the serving certificate using the same CA. |
| `cronJob` | Helm bootstraps the CA and Service certificate | A CronJob renews the serving certificate before expiry. When the CA needs replacement, it adds both CAs to the webhook trust bundle, replaces the certificate, waits for the webhook Deployment, then removes the old CA. |
| `certmanager` | A cert-manager Certificate uses `tls.certManager.issuerRef` | cert-manager renews the Secret and cainjector supplies the webhook CA bundle. The issuer must populate `ca.crt`. |

The serving certificate covers the webhook Service name and its namespace and
`.svc` DNS names. The image repository and tag are configurable with `image`;
the default `addon-webhook:dev` is a local development image.
