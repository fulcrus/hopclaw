---
name: kubernetes
description: Manage Kubernetes clusters, pods, deployments, and services via kubectl
homepage: https://kubernetes.io/docs/reference/kubectl/
user-invocable: true
command-dispatch: tool
command-tool: kubernetes.run
command-arg-mode: raw
metadata:
  openclaw:
    skillKey: infra.kubernetes
    emoji: "\u2638\uFE0F"
    requires:
      bins:
        - kubectl
    always: false
---
# Kubernetes

Manage Kubernetes resources using `kubectl`.

## Capabilities

- List and describe pods, deployments, services, and other resources
- View pod logs and events
- Apply, create, and delete resources from YAML manifests
- Scale deployments and stateful sets
- Execute commands inside pods
- Manage namespaces, config maps, and secrets
- Port-forward to local machine

## Usage

When the user asks about Kubernetes operations, use the `kubectl` CLI.
Always check the current context and namespace before making changes.

### Context and Namespace

```bash
# View current context
kubectl config current-context

# List all contexts
kubectl config get-contexts

# Switch context
kubectl config use-context my-cluster

# Set default namespace
kubectl config set-context --current --namespace=my-namespace
```

### Viewing Resources

```bash
# List pods in current namespace
kubectl get pods

# List pods across all namespaces
kubectl get pods -A

# Detailed pod info
kubectl describe pod my-pod

# List deployments with wide output
kubectl get deployments -o wide

# Get resource as YAML
kubectl get deployment my-app -o yaml

# List services
kubectl get svc

# View events sorted by time
kubectl get events --sort-by='.lastTimestamp'
```

### Logs and Debugging

```bash
# Pod logs
kubectl logs my-pod

# Follow logs with tail
kubectl logs -f --tail=100 my-pod

# Logs from a specific container in multi-container pod
kubectl logs my-pod -c sidecar

# Previous container logs (after crash)
kubectl logs my-pod --previous

# Execute command in pod
kubectl exec -it my-pod -- sh

# Port forward
kubectl port-forward svc/my-service 8080:80
```

### Managing Resources

```bash
# Apply a manifest
kubectl apply -f deployment.yaml

# Scale a deployment
kubectl scale deployment my-app --replicas=3

# Rollout status
kubectl rollout status deployment/my-app

# Rollback a deployment
kubectl rollout undo deployment/my-app

# Delete a resource
kubectl delete pod my-pod

# Restart a deployment (rolling restart)
kubectl rollout restart deployment/my-app
```

### ConfigMaps and Secrets

```bash
# Create configmap from file
kubectl create configmap my-config --from-file=config.yaml

# Create secret from literal
kubectl create secret generic my-secret --from-literal=password=s3cr3t

# View configmap data
kubectl get configmap my-config -o jsonpath='{.data}'
```

### Resource Usage

```bash
# Node resource usage
kubectl top nodes

# Pod resource usage
kubectl top pods

# Pod resource usage sorted by memory
kubectl top pods --sort-by=memory
```

## Error Handling

- If "the connection to the server was refused", the cluster is unreachable. Check kubeconfig and VPN.
- If "forbidden", the user lacks RBAC permissions. Show which role is needed.
- If pods are in CrashLoopBackOff, check logs with `--previous` and describe the pod for events.
- If "resource not found", verify the namespace with `-n namespace`.

## Security

- Never display secret values in plain text; use `kubectl get secret -o yaml` only when the user explicitly requests it.
- Always confirm the target context and namespace before destructive operations like `delete` or `scale --replicas=0`.
- Warn before applying changes to production contexts.
- Do not store kubeconfig credentials in command output or logs.
