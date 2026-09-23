# Kubernetes samples

These manifests show the current `egressfox.io/v1alpha1` shape for one adaptive
`ProxyPool`, one explicitly managed sing-box-compatible `EgressGateway`, and one
backward-compatible BYO Gateway in the same namespace.

[The managed HTTP source example](egressfox_v1alpha1_http_proxypool.yaml) is
available separately and is not applied by `kubectl apply -k`. It shows the
same-namespace URL and Authorization Secret references, conditional refresh and
bounded fallback. Replace its placeholder values before applying it explicitly.

The Secret values are intentionally fake. `example.test` is reserved for examples,
so the sample will not become Ready until both Secret values are replaced through
your normal secret-management workflow:

- `sample-subscription` / `nodes`: a supported URI list or Base64 URI list;
- `sample-target` / `url`: an authorized HTTP(S) endpoint with the expected status.

Never commit real replacements. Review the [operator guide](../../docs/operations/kubernetes.md)
before applying the manifests, especially its Secret, private-network, CRD, PVC,
managed-authentication, and BYO runtime requirements.

After installing the chart in the target namespace, apply the sample shape with:

```sh
kubectl apply -k config/samples
```

The operator creates an authenticated SOCKS Service and exact-generation workload
for `sample`. Read its Service and client-auth Secret names from status. For
`sample-byo`, the operator publishes `sample-byo-engine-config` but never creates,
adopts, restarts, or confirms activation of a user workload.
