# demo-app

A small web app for the Tenantry demo (PRODUCT-PLAN §28): one Deployment, a PersistentVolumeClaim on
`standard`, and a ClusterIP Service. There is no Ingress: Workspaces cannot write one. The app goes on the
internet through a Route made on the portal.

Each start of the Pod adds a line to a page kept on the volume. After a restart, or after the Pod is deleted,
the earlier lines are still there.

```sh
# kubeconfig from the portal (Access tab); <ns> is a Namespace of the Workspace, e.g. acme-solar-frontend
helm install web deploy/demo-app -n <ns>
kubectl -n <ns> get pods,pvc,svc
kubectl -n <ns> delete pod -l app.kubernetes.io/instance=web   # restart: the page keeps its lines
```

Then, on the portal: Routes → Create, Namespace `<ns>`, Service `web`, port `80`. The app is served at
`https://<route>-<org>-<workspace>.mvp.tenantry.site`.

What it fits:
- Every container states requests and limits, because Workspaces refuse BestEffort Pods. The total is
  100m CPU, 64Mi memory and 1Gi storage, counted against the Workspace's quota.
- It runs as non-root with no capability, which also satisfies the `restricted` Pod Security level.
- Images come from docker.io, which is on the registry allow-list.
- Change `storage.className` if the Workspace allows another StorageClass, and `message` for the page's title.
- `service.name` names the Service (the release name when empty), e.g. `helm upgrade web deploy/demo-app -n <ns> --reuse-values --set service.name=web11`; the Route must then name that Service.
