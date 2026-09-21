# Kubernetes samples

These manifests show the current `egressfox.io/v1alpha1` shape for one adaptive
`ProxyPool` and one sing-box-compatible `EgressGateway` in the same namespace.

The Secret values are intentionally fake. `example.test` is reserved for examples,
so the sample will not become Ready until both Secret values are replaced through
your normal secret-management workflow:

- `sample-subscription` / `nodes`: a supported URI list or Base64 URI list;
- `sample-target` / `url`: an authorized HTTP(S) endpoint with the expected status.

Never commit real replacements. Review the [operator guide](../../docs/operations/kubernetes.md)
before applying the manifests, especially its Secret, private-network, CRD, PVC, and
BYO runtime requirements.

After installing the chart in the target namespace, apply the sample shape with:

```sh
kubectl apply -k config/samples
```

The operator publishes `sample-engine-config`; it does not create, expose, restart,
or confirm activation of an engine workload.
