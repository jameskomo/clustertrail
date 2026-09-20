#!/usr/bin/env python3
"""Writes the Secrets a Helm release is made of.

Helm keeps each revision in a Secret whose payload is base64 of gzip of JSON.
Reproducing that exactly is what lets the Helm screen be tested without
installing Helm or reaching a chart repository.
"""
import base64, gzip, json, sys

def manifest(tag: str, replicas: int) -> str:
    return f"""---
# Source: shop-frontend/templates/service.yaml
apiVersion: v1
kind: Service
metadata:
  name: shop-frontend
  namespace: shop
spec:
  ports:
    - port: 80
      targetPort: 80
  selector:
    app: shop-frontend
---
# Source: shop-frontend/templates/deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: shop-frontend
  namespace: shop
spec:
  replicas: {replicas}
  selector:
    matchLabels:
      app: shop-frontend
  template:
    metadata:
      labels:
        app: shop-frontend
    spec:
      containers:
        - name: web
          image: nginx:{tag}
"""

def release(version: int, chart: str, tag: str, replicas: int, status: str, description: str) -> dict:
    return {
        "name": "shop-frontend", "namespace": "shop", "version": version,
        "info": {
            "first_deployed": "2026-09-18T12:00:00Z",
            "last_deployed": f"2026-09-{17 + version}T12:00:00Z",
            "deleted": "", "description": description, "status": status,
            "notes": "Thanks for installing shop-frontend.\n\nGet the URL:\n"
                     "  kubectl port-forward svc/shop-frontend 8080:80 -n shop\n",
        },
        "chart": {"metadata": {"name": "shop-frontend", "version": chart,
                               "appVersion": "2.1.0", "apiVersion": "v2"}},
        "config": {"replicaCount": replicas,
                   "image": {"repository": "nginx", "tag": tag},
                   "ingress": {"enabled": False}},
        "manifest": manifest(tag, replicas),
    }

revisions = [
    release(1, "1.4.2", "1.27-alpine", 2, "superseded", "Install complete"),
    release(2, "1.5.0", "1.28-alpine", 3, "deployed", "Upgrade complete"),
]

docs = []
for rel in revisions:
    payload = base64.b64encode(gzip.compress(json.dumps(rel).encode())).decode()
    docs.append({
        "apiVersion": "v1", "kind": "Secret",
        "metadata": {
            "name": f"sh.helm.release.v1.shop-frontend.v{rel['version']}",
            "namespace": "shop",
            "labels": {"owner": "helm", "name": "shop-frontend",
                       "status": rel["info"]["status"], "version": str(rel["version"])},
        },
        "type": "helm.sh/release.v1",
        "stringData": {"release": payload},
    })

json.dump({"apiVersion": "v1", "kind": "List", "items": docs}, sys.stdout)
