# Sample repository for the Drift screen

Point Drift at this directory to see all three kinds of drift against the kind
cluster created by `scripts/dev.sh`:

- `checkout.yaml` is behind the live Deployment, so it reports **differs from Git**.
- `frontend-service.yaml` is not in the cluster until you create it, so it
  reports **in Git, not running**. Creating it from the screen goes through the
  review gate like any other change; delete the Service again with
  `kubectl delete svc shop-frontend -n shop` to reset.
- Everything else running in `shop` has no file here, so with the unmanaged
  checkbox ticked it reports **running, not in Git**.
