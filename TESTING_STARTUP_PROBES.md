# Knative status probe testing

## Setup
```bash
# cert-manager, net-certmanager
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.13.2/cert-manager.yaml
kubectl wait --for=condition=Established --all crd
kubectl wait --for=condition=Available -n cert-manager --all deployments

# serving
ko apply --selector knative.dev/crd-install=true -Rf config/core/
kubectl wait --for=condition=Established --all crd
ko apply -Rf config/core/

# kourier
ko apply -Rf config

# config patches
kubectl patch configmap/config-network \
  --namespace knative-serving \
  --type merge \
  --patch '{"data":{"ingress-class":"kourier.ingress.networking.knative.dev"}}'
kubectl patch configmap/config-domain \
  --namespace knative-serving \
  --type merge \
  --patch "{\"data\":{\"10.89.0.200.sslip.io\":\"\"}}"

# more logs
kubectl patch configmap/config-observability \
  --namespace knative-serving \
  --type merge \
  --patch '{"data":{"logging.enable-request-log":"true","logging.enable-probe-request-log":"true"}}'
kubectl patch configmap/config-logging \
  --namespace knative-serving \
  --type merge \
  --patch '{"data":{"loglevel.controller":"debug", "loglevel.queueproxy":"debug"}}'
  
# deploy curl
kubectl apply -f yaml/curl.yaml
```

## Testing with defaults
```bash
kubectl delete -n default ksvc runtime
kubectl apply -f 3-startup-probes/1-ksvc-default.yaml # ok
```

## Testing defaults with a startup-probe

```bash
kubectl delete -n default ksvc runtime
kubectl apply -f 3-startup-probes/2-ksvc-startup-probe.yaml # ok
```

## Testing with a startup-probe toggles
```bash
kubectl delete -n default ksvc runtime
ko apply -f 3-startup-probes/3-ksvc-startup-probe-initially-down-600s.yaml
```

```text
# Knative Service is not ready:
0s          Warning   Unhealthy               pod/runtime-00008-deployment-c4dc5dff8-d74gj     Startup probe failed: HTTP probe failed with statuscode: 503

# But Queue-Proxy is probing for readiness, which is ok:
queue-proxy {"httpRequest": {"requestMethod": "GET", "requestUrl": "/healthz", "requestSize": "0", "status": 200, "responseSize": "5", "userAgent": "Knative-Activator-Probe", "remoteIp": "10.244.1.8:59952", "serverIp": "10.244.1.14", "referer": "", "latency": "0.000932752s", "protocol": "HTTP/1.1"}, "traceId": "]"}
```

```bash
# Knative Service is in:
kubectl get ksvc -n default runtime -o jsonpath="{.status.conditions}" | jq
```
```json
[
  {
    "lastTransitionTime": "2024-06-06T09:24:51Z",
    "status": "Unknown",
    "type": "ConfigurationsReady"
  },
  {
    "lastTransitionTime": "2024-06-06T09:24:51Z",
    "message": "Configuration \"runtime\" is waiting for a Revision to become ready.",
    "reason": "RevisionMissing",
    "status": "Unknown",
    "type": "Ready"
  },
  {
    "lastTransitionTime": "2024-06-06T09:24:51Z",
    "message": "Configuration \"runtime\" is waiting for a Revision to become ready.",
    "reason": "RevisionMissing",
    "status": "Unknown",
    "type": "RoutesReady"
  }
]
```
```bash
# SKS is in:
kubectl get sks -n default runtime-00001 -o jsonpath="{.status.conditions}" | jq
```
```json
[
  {
    "lastTransitionTime": "2024-06-06T09:24:51Z",
    "message": "Revision is backed by Activator",
    "reason": "ActivatorEndpointsPopulated",
    "severity": "Info",
    "status": "True",
    "type": "ActivatorEndpointsPopulated"
  },
  {
    "lastTransitionTime": "2024-06-06T09:24:51Z",
    "message": "K8s Service is not ready",
    "reason": "NoHealthyBackends",
    "status": "Unknown",
    "type": "EndpointsPopulated"
  },
  {
    "lastTransitionTime": "2024-06-06T09:24:51Z",
    "message": "K8s Service is not ready",
    "reason": "NoHealthyBackends",
    "status": "Unknown",
    "type": "Ready"
  }
]

```
```bash
# and the service cannot yet be called from outside of the cluster:
curl -i http://runtime.default.10.89.0.200.sslip.io 
```
```text
HTTP/1.1 404 Not Found
date: Thu, 06 Jun 2024 08:47:10 GMT
server: envoy
content-length: 0
```
```bash
# and also not from inside of the cluster (K8s service is missing)
kubectl exec deployment/curl -n default -it -- curl -iv http://runtime.default.svc.cluster.local
```
```text
* Could not resolve host: runtime.default.svc.cluster.local
* Closing connection
curl: (6) Could not resolve host: runtime.default.svc.cluster.local
command terminated with exit code 6
```

```bash
# Toggle the startup probe
POD_IP=$(kubectl get -n default $(kubectl get po -n default -o name -l app=runtime-00001) --template '{{.status.podIP}}')
kubectl exec deployment/curl -n default -it -- curl -iv http://$POD_IP:8080/toggleStartup
```
```bash
# and the service cannot yet be called from outside of the cluster:
curl -i http://runtime.default.10.89.0.200.sslip.io 
```
```text
HTTP/1.1 200 OK
```
```bash
# and also not from inside of the cluster (K8s service is missing)
kubectl exec deployment/curl -n default -it -- curl -i http://runtime.default.svc.cluster.local
```
```text
HTTP/1.1 200 OK
```

## Testing with a startup-probe toggles, startup probe longer than progress deadline timeout
```bash
kubectl delete -n default ksvc runtime
ko apply -f 3-startup-probes/3-ksvc-startup-probe-initially-down-10s.yaml
```
```bash
# wait for the pod to be scaled down again
sleep 15 # wait for system to detect the pod is not ready
sleep 30 # wait for sigterm hook
sleep 15 # buffer
```

```bash
# Knative Service is in:
kubectl get ksvc -n default runtime -o jsonpath="{.status.conditions}" | jq
```
```json
[
  {
    "lastTransitionTime": "2024-06-06T09:28:01Z",
    "message": "Revision \"runtime-00001\" failed with message: Initial scale was never achieved.",
    "reason": "RevisionFailed",
    "status": "False",
    "type": "ConfigurationsReady"
  },
  {
    "lastTransitionTime": "2024-06-06T09:27:21Z",
    "message": "Configuration \"runtime\" does not have any ready Revision.",
    "reason": "RevisionMissing",
    "status": "False",
    "type": "Ready"
  },
  {
    "lastTransitionTime": "2024-06-06T09:27:21Z",
    "message": "Configuration \"runtime\" does not have any ready Revision.",
    "reason": "RevisionMissing",
    "status": "False",
    "type": "RoutesReady"
  }
]
```
```bash
# SKS is in:
kubectl get sks -n default runtime-00001 -o jsonpath="{.status.conditions}" | jq
```
```json
[
  {
    "lastTransitionTime": "2024-06-06T09:27:10Z",
    "message": "Revision is backed by Activator",
    "reason": "ActivatorEndpointsPopulated",
    "severity": "Info",
    "status": "True",
    "type": "ActivatorEndpointsPopulated"
  },
  {
    "lastTransitionTime": "2024-06-06T09:27:10Z",
    "message": "K8s Service is not ready",
    "reason": "NoHealthyBackends",
    "status": "Unknown",
    "type": "EndpointsPopulated"
  },
  {
    "lastTransitionTime": "2024-06-06T09:27:10Z",
    "message": "K8s Service is not ready",
    "reason": "NoHealthyBackends",
    "status": "Unknown",
    "type": "Ready"
  }
]
```
```bash
# and the service cannot yet be called from outside of the cluster:
curl -i http://runtime.default.10.89.0.200.sslip.io 
```
```text
HTTP/1.1 404 Not Found
date: Thu, 06 Jun 2024 08:47:10 GMT
server: envoy
content-length: 0
```
```bash
# and also not from inside of the cluster (K8s service is missing)
kubectl exec deployment/curl -n default -it -- curl -iv http://runtime.default.svc.cluster.local
```

## Testing vanilla K8s deployment with startup probe and progress deadline timeout

```bash
ko apply -f 3-startup-probes/4-deploy-startup-probe-progress-deadline.yaml
```

## Testing with a startup-probe toggles, startup probe longer than progress deadline timeout, with patched Serving
```bash
kubectl delete -n default ksvc runtime
ko apply -f 3-startup-probes/3-ksvc-startup-probe-initially-down-600s.yaml
```
```bash
# check calculated progress deadline
kubectl get deploy runtime-00001-deployment -n default -o jsonpath="{.spec.progressDeadlineSeconds}"
```
```text
3610
```
```bash
# wait for the pod to NOT be scaled down again
sleep 15 # wait for system to detect the pod is not ready
sleep 30 # wait for sigterm hook
sleep 15 # buffer
```

```bash
# toggle the startup probe
POD_IP=$(kubectl get -n default $(kubectl get po -n default -o name -l app=runtime-00001) --template '{{.status.podIP}}')
kubectl exec deployment/curl -n default -it -- curl -iv http://$POD_IP:8080/toggleStartup
```
```text
The service should transition to ready and NOT scale down to zero:
```
```bash
# and the service can now be called from outside of the cluster:
curl -i http://runtime.default.10.89.0.200.sslip.io 
```
```text
HTTP/1.1 200 OK
```
```bash
# and from inside of the cluster
kubectl exec deployment/curl -n default -it -- curl -iv http://runtime.default.svc.cluster.local
```
```text
HTTP/1.1 200 OK
```