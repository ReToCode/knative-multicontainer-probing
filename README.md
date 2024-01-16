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
```

## Testing single containers

```bash
kubectl apply -f 1-single-container/xx.yaml
```

```bash
kubectl apply -f 1-single-container/1-ksvc-default.yaml
kubectl apply -f 1-single-container/6-ksvc-exec-probe-readiness.yaml
```

## Testing LivenessProbes with multiple containers

* Disable the webhook validation upfront

```bash
kubectl apply -f 2-multi-container/2-ksvc-default-liveness.yaml
```

```bash
ko apply -f 2-multi-container/4-ksvc-liveness-toggle.yaml

# main container liveness: false
curl  -iv http://test-probe.default.172.17.0.100.sslip.io/toggle

# Logs
# K8s will restart the main container
queue-proxy {"severity":"INFO","timestamp":"2024-01-16T14:24:09.915295493Z","logger":"queueproxy","caller":"sharedmain/handlers.go:107","message":"Attached drain handler from user-container&{GET /wait-for-drain HTTP/1.1 1 1 map[Accept:[*/*] Accept-Encoding:[gzip] User-Agent:[kube-lifecycle/1.28]] {} <nil> 0 ] false 10.42.0.14:8022 map] map] <nil> map] 10.42.0.1:46226 /wait-for-drain <nil> <nil> <nil> 0x40002c4500}","commit":"d96dabb-dirty","knative.dev/key":"default/test-probe-00005","knative.dev/pod":"test-probe-00005-deployment-68f4d64498-9bkhg"}
Stream closed EOF for default/test-probe-00005-deployment-68f4d64498-9bkhg (first-container)
second-container Liveness probe called, responding with:  true

# use a curl pod to deactivate different probes
kubectl exec deployment/curl -n default -it -- curl -iv http://<pod-ip>:8090/toggle

# Logs
# K8s will restart the sidecar container
first-container Starting server. Listening on port:  8080
second-container Starting server. Listening on port:  8090
queue-proxy {"severity":"INFO","timestamp":"2024-01-16T14:30:36.643425616Z","logger":"queueproxy","caller":"sharedmain/main.go:268","message":"Starting queue-proxy","commit":"d96dabb-dirty","knative.dev/key":"default/test-probe-00005","knative.dev/pod":"test-probe-00005-deployment-68f4d64498-6jqtf"}
queue-proxy {"severity":"INFO","timestamp":"2024-01-16T14:30:36.643470491Z","logger":"queueproxy","caller":"sharedmain/main.go:274","message":"Starting http server metrics:9090","commit":"d96dabb-dirty","knative.dev/key":"default/test-probe-00005","knative.dev/pod":"test-probe-00005-deployment-68f4d64498-6jqtf"}
queue-proxy {"severity":"INFO","timestamp":"2024-01-16T14:30:36.643478074Z","logger":"queueproxy","caller":"sharedmain/main.go:274","message":"Starting http server main:8012","commit":"d96dabb-dirty","knative.dev/key":"default/test-probe-00005","knative.dev/pod":"test-probe-00005-deployment-68f4d64498-6jqtf"}
queue-proxy {"severity":"INFO","timestamp":"2024-01-16T14:30:36.643548199Z","logger":"queueproxy","caller":"sharedmain/main.go:274","message":"Starting http server admin:8022","commit":"d96dabb-dirty","knative.dev/key":"default/test-probe-00005","knative.dev/pod":"test-probe-00005-deployment-68f4d64498-6jqtf"}
second-container Liveness probe called, responding with:  false
queue-proxy {"severity":"INFO","timestamp":"2024-01-16T14:31:36.389518774Z","logger":"queueproxy","caller":"sharedmain/handlers.go:107","message":"Attached drain handler from user-container&{GET /wait-for-drain HTTP/1.1 1 1 map[Accept:[*/*] Accept-Encoding:[gzip] User-Agent:[kube-lifecycle/1.28]] {} <nil> 0 ] false 10.42.0.15:8022 map] map] <nil> map] 10.42.0.1:42442 /wait-for-drain <nil> <nil> <nil> 0x40006b3c20}","commit":"d96dabb-dirty","knative.dev/key":"default/test-probe-00005","knative.dev/pod":"test-probe-00005-deployment-68f4d64498-6jqtf"}
Stream closed EOF for default/test-probe-00005-deployment-68f4d64498-6jqtf (second-container)
first-container Liveness probe called, responding with:  true
second-container Liveness probe called, responding with:  true
```

* Liveness probes can be enabled without interference, <span style="color:red">but no additional header is injected</span>. So we at least would need to fix this, otherwise LivenessProbes can not target Queue-Proxy.
* But this can cause some race conditions:
  * Queue-Proxy (and other Knative components) will not know about the UC being not live and being restarted. We'll see `HTTP/1.1 503 Service Unavailable` when calling the Knative Service
  * The same applies for sidecars. Queue-Proxy (and other Knative components) will not know about this. Depending on what the sidecar does, this can cause issues.

## Testing ReadinessProbes with multiple containers

If we allow ReadinessProbes for additional containers, we have at least these race conditions:

- If a sidecar container does not become ready, the `Revision`, `Configuration` and `KService` are in unknown status.
- `KIngress` object is not created.


