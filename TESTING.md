# Knative multi-container probing

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
  --patch "{\"data\":{\"172.17.0.100.sslip.io\":\"\"}}"

# more logs
kubectl patch configmap/config-observability \
  --namespace knative-serving \
  --type merge \
  --patch '{"data":{"logging.enable-request-log":"true","logging.enable-probe-request-log":"true"}}'
kubectl patch configmap/config-logging \
  --namespace knative-serving \
  --type merge \
  --patch '{"data":{"loglevel.controller":"debug", "loglevel.queueproxy":"debug"}}'
  
# enable multi-container probing
kubectl patch configmap/config-features \
  --namespace knative-serving \
  --type merge \
  --patch '{"data":{"multi-container-probing":"enabled"}}'
```

## Testing single containers

```bash
kubectl apply -f 1-single-container/1-ksvc-default.yaml # ok

kubectl apply -f 1-single-container/1-ksvc-default-unhealthy.yaml

user-container 2024/01/31 07:05:20 Started...
user-container 2024/01/31 07:05:30 Crashed...
queue-proxy {"severity":"DEBUG","timestamp":"2024-01-31T07:05:10.376786432Z","caller":"logging/config.go:116","message":"Successfully created the logger."}
queue-proxy {"severity":"DEBUG","timestamp":"2024-01-31T07:05:10.376825391Z","caller":"logging/config.go:117","message":"Logging level set to: debug"}
queue-proxy {"severity":"INFO","timestamp":"2024-01-31T07:05:10.377093354Z","logger":"queueproxy","caller":"sharedmain/main.go:271","message":"Starting queue-proxy","commit":"20a8d97-dirty","knative.dev/key":"default/runtime-00002","knative.dev/pod":"runtime-00002-deployment-646b66dd57-qk9cg"}
queue-proxy {"severity":"INFO","timestamp":"2024-01-31T07:05:10.377166147Z","logger":"queueproxy","caller":"sharedmain/main.go:277","message":"Starting http server main:8012","commit":"20a8d97-dirty","knative.dev/key":"default/runtime-00002","knative.dev/pod":"runtime-00002-deployment-646b66dd57-qk9cg"}
queue-proxy {"severity":"INFO","timestamp":"2024-01-31T07:05:10.377171814Z","logger":"queueproxy","caller":"sharedmain/main.go:277","message":"Starting http server admin:8022","commit":"20a8d97-dirty","knative.dev/key":"default/runtime-00002","knative.dev/pod":"runtime-00002-deployment-646b66dd57-qk9cg"}
queue-proxy {"severity":"INFO","timestamp":"2024-01-31T07:05:10.37711848Z","logger":"queueproxy","caller":"sharedmain/main.go:277","message":"Starting http server metrics:9090","commit":"20a8d97-dirty","knative.dev/key":"default/runtime-00002","knative.dev/pod":"runtime-00002-deployment-646b66dd57-qk9cg"}
queue-proxy aggressive probe error (failed 201 times): dial tcp 127.0.0.1:8080: connect: connection refused
queue-proxy context deadline exceeded
queue-proxy aggressive probe error (failed 201 times): dial tcp 127.0.0.1:8080: connect: connection refused
queue-proxy context deadline exceeded
Stream closed EOF for default/runtime-00002-deployment-646b66dd57-qk9cg (user-container)
queue-proxy aggressive probe error (failed 202 times): dial tcp 127.0.0.1:8080: connect: connection refused

kubectl apply -f 1-single-container/2-ksvc-default-liveness.yaml # ok
kubectl apply -f 1-single-container/3-ksvc-default-readiness.yaml # ok
kubectl apply -f 1-single-container/4-ksvc-tcp-probe-readiness.yaml # ok
kubectl apply -f 1-single-container/5-ksvc-tcp-probe-liveness.yaml # ok
kubectl apply -f 1-single-container/6-ksvc-exec-probe-readiness.yaml # ok
kubectl apply -f 1-single-container/7-ksvc-exec-probe-liveness.yaml # ok
kubectl apply -f 1-single-container/8-ksvc-grpc-probe-readiness.yaml # ok
kubectl apply -f 1-single-container/9-ksvc-grpc-probe-liveness.yaml # ok
kubectl apply -f 1-single-container/12-cross-port-readiness.yaml # ok
```

## Testing with multiple containers

```bash
kubectl apply -f 2-multi-container/1-ksvc-default.yaml # ok
kubectl apply -f 2-multi-container/2-ksvc-default-liveness.yaml # ok
kubectl apply -f 2-multi-container/3-ksvc-default-readiness.yaml # ok
```

### Testing with liveness toggles
```bash
ko apply -f 2-multi-container/4-ksvc-liveness-toggle.yaml

# 1) toggle: first container
curl  -iv http://test-probe.default.172.17.0.100.sslip.io/toggleLive
# we see the same behaviour as in README.md
# - K8s will restart the first-container
# - Requests to that pod will fail, as QP does not know about that container and it's health
# - Users will need to use readiness-probes as well to stop QP from sending traffic

# 2) toggle: second-container
kubectl exec deployment/curl -n default -it -- curl -iv http://10.42.0.110:8090/toggleLive
# this will still work, as long as the second-container is not in the data-path

```

### Testing with readiness toggles

```bash
ko apply -f 2-multi-container/5-ksvc-readiness-toggle.yaml
# or
ko apply -f 2-multi-container/6-ksvc-readiness-toggle-serve-mode.yaml

# toggle readiness in BOTH containers
kubectl exec deployment/curl -n default -it -- curl -iv http://10.42.0.118:8080/toggleReady
kubectl exec deployment/curl -n default -it -- curl -iv http://10.42.0.118:8090/toggleReady

# Knative Service is ready
k get ksvc,configuration,king

NAME                                     URL                                               LATESTCREATED      LATESTREADY        READY   REASON
service.serving.knative.dev/test-probe   http://test-probe.default.172.17.0.100.sslip.io   test-probe-00001   test-probe-00001   True

NAME                                           LATESTCREATED      LATESTREADY        READY   REASON
configuration.serving.knative.dev/test-probe   test-probe-00001   test-probe-00001   True

NAME                                                 READY   REASON
ingress.networking.internal.knative.dev/test-probe   True

# Knative Service works
curl -iv http://test-probe.default.172.17.0.100.sslip.io
HTTP/1.1 200 OK

# set the first container to not ready
kubectl exec deployment/curl -n default -it -- curl -iv http://10.42.0.118:8080/toggleReady

# QP knows about it and starts polling again
first-container Readiness probe called, responding with:  false
first-container Readiness probe called, responding with:  false
queue-proxy {"httpRequest": {"requestMethod": "GET", "requestUrl": "/", "requestSize": "0", "status": 503, "responseSize": "0", "userAgent": "kube-probe/1.28", "remoteIp": "10.42.0.1:57566", "serverIp": "10.42.0.118", "referer": "", "latency": "10.000105493s", "protocol": "HTTP/1.1"}, "traceId": "]"}
queue-proxy aggressive probe error (failed 201 times): HTTP probe did not respond Ready, got status code: 503
queue-proxy context deadline exceeded
queue-proxy {"httpRequest": {"requestMethod": "GET", "requestUrl": "/", "requestSize": "0", "status": 503, "responseSize": "0", "userAgent": "kube-probe/1.28", "remoteIp": "10.42.0.1:50770", "serverIp": "10.42.0.118", "referer": "", "latency": "0.000192956s", "protocol": "HTTP/1.1"}, "traceId": "]"}

# QPs own readiness-probe starts to fail:
kubectl exec deployment/curl -n default -it -- curl -iv http://10.42.0.111:8012 -H "K-Network-Probe: queue" -H "K-Kubelet-Probe: value"
HTTP/1.1 503 Service Unavailable

# Knative will only reflect the error in the SKS
k get sks
NAME                                                                 MODE    ACTIVATORS   SERVICENAME        PRIVATESERVICENAME         READY     REASON
serverlessservice.networking.internal.knative.dev/test-probe-00001   Proxy   3            test-probe-00001   test-probe-00001-private   Unknown   NoHealthyBackends

# Requests will now no longer be sent to the actual container (only to activator)
curl -iv http://test-probe.default.172.17.0.100.sslip.io

# The same thing happens when the readiness probe on the second container fails
# make first one ready again
kubectl exec deployment/curl -n default -it -- curl -iv http://10.42.0.111:8080/toggleReady

# make the second un-ready
kubectl exec deployment/curl -n default -it -- curl -iv http://10.42.0.111:8090/toggleReady

kubectl exec deployment/curl -n default -it -- curl -iv http://10.42.0.111:8012 -H "K-Network-Probe: queue" -H "K-Kubelet-Probe: value"
HTTP/1.1 503 Service Unavailable

k get sks
NAME                                                                 MODE    ACTIVATORS   SERVICENAME        PRIVATESERVICENAME         READY     REASON
serverlessservice.networking.internal.knative.dev/test-probe-00001   Proxy   3            test-probe-00001   test-probe-00001-private   Unknown   NoHealthyBackends

# Requests will now no longer be sent to the actual container (only to activator)
curl -iv http://test-probe.default.172.17.0.100.sslip.io
```
