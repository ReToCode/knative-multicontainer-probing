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
  
# enable request logging
## Activator and Q-P
kubectl patch configmap/config-observability \
  --namespace knative-serving \
  --type merge \
  --patch '{"data":{"logging.enable-request-log":"true"}}'
  
## Kourier gateway
kubectl patch configmap/config-kourier \
  --namespace knative-serving \
  --type merge \
  --patch '{"data":{"logging.enable-request-log":"true"}}'
```

## Testing single containers

```bash
kubectl apply -f 1-single-container/xx.yaml
```

```bash
kubectl apply -f 1-single-container/1-ksvc-default.yaml
kubectl apply -f 1-single-container/6-ksvc-exec-probe-readiness.yaml
```

### Cross port probing

It is possible to test the health on another port than the data-traffic:

```bash
kubectl apply -f 1-single-container/12-cross-port-readiness.yaml

# The tests will be routed through Queue-Proxy:
#  - name: SERVING_READINESS_PROBE
#    value: '{"httpGet":{"port":8090,"host":"127.0.0.1","scheme":"HTTP","httpHeaders":[{"name":"K-Kubelet-Probe","value":"queue"}]},"successThreshold":1}'
```

### Exec probes

#### Readiness

```bash
ko apply -f 1-single-container/10-ksvc-exec-probe-readiness.yaml

# this creates a healthy server, all is ok

# start failing the exec probes
curl -iv http://runtime.default.172.17.0.100.sslip.io/toggleExec

# Knative is happy
k get ksvc,configuration,revision,king,sks

NAME                                  URL                                            LATESTCREATED   LATESTREADY     READY   REASON
service.serving.knative.dev/runtime   http://runtime.default.172.17.0.100.sslip.io   runtime-00001   runtime-00001   True

NAME                                        LATESTCREATED   LATESTREADY     READY   REASON
configuration.serving.knative.dev/runtime   runtime-00001   runtime-00001   True

NAME                                         CONFIG NAME   K8S SERVICE NAME   GENERATION   READY   REASON   ACTUAL REPLICAS   DESIRED REPLICAS
revision.serving.knative.dev/runtime-00001   runtime                          1            True             0                 1

NAME                                              READY   REASON
ingress.networking.internal.knative.dev/runtime   True

NAME                                                              MODE    ACTIVATORS   SERVICENAME     PRIVATESERVICENAME      READY     REASON
serverlessservice.networking.internal.knative.dev/runtime-00001   Proxy   3            runtime-00001   runtime-00001-private   Unknown   NoHealthyBackends

# Queue-Proxy readiness is ok
kubectl exec deployment/curl -n default -it -- curl -iv http://10.42.0.29:8012 -H "K-Network-Probe: queue" -H "K-Kubelet-Probe: value"
HTTP/1.1 200 OK

# Kubernetes is not happy, endpoints are removed from private service
k get endpoints,pod
NAME                              ENDPOINTS                         AGE
endpoints/runtime-00001           10.42.0.20:8012,10.42.0.20:8112   2m58s
endpoints/runtime-00001-private                                     2m58s

NAME                                          READY   STATUS    RESTARTS       AGE
pod/runtime-00001-deployment-c56d87d5-rj5nl   1/2     Running   0              2m58s

# we cannot call the service any longer, as traffic will be sent do activator (10.42.0.20) 
# activator is holding the requests until the probe is ready again until you get
HTTP/1.1 504 Gateway Timeout
activator request timeout%
```

#### Summary
* This works as expected traffic wise, even though Knative does not reflect the state properly
* Queue-Proxy probe is reflecting the "wrong" state
* This only works because Knative threads exec readiness probes differently. Normally, activator would send requests to all Pods that pass Queue-Proxy readiness checks. In this case, Queue-Proxy readiness is ok, but traffic is still not forwarded. When an exec readiness probe is present, Activator waits for the endpoints to be populated as "ready" (`addresses` field) by K8s.
* The situation will not resolve itself, requests will be buffered until that probe is good again


#### Liveness

```bash
ko apply -f 1-single-container/11-ksvc-exec-probe-liveness.yaml

# this creates a healthy server, all is ok

# start failing the liveness probe
curl -iv http://runtime.default.172.17.0.100.sslip.io/toggleExec

# Knative is happy

# Kubernetes is not happy for a short time and will restart the user-container

# Queue-Proxy tries to forward requests, but will error out:
queue-proxy {"severity":"ERROR","timestamp":"2024-01-18T14:36:05.753218441Z","logger":"queueproxy","caller":"network/error_handler.go:33","message":"error reverse proxying request; sockstat: sockets: used 8\nTCP: inuse 0 orphan 0 tw 15 alloc 185 mem 0\nUDP: inuse 0 mem 0\nUDPLITE: inuse 0\nRAW: inuse 0\nFRAG: inuse 0 memory 0\n","commit":"d96dabb-dirty","knative.dev/key":"default/runtime-00001","knative.dev/pod":"runtime-00001-deployment-7b9c49d676-dlxmt","error":"dial tcp 127.0.0.1:8080: connect: connection refused","stacktrace":"knative.dev/pkg/network.ErrorHandler.func1\n\tknative.dev/pkg@v0.0.0-20240115132401-f95090a164db/network/error_handler.go:33\nnet/http/httputil.(*ReverseProxy).ServeHTTP\n\tnet/http/httputil/reverseproxy.go:475\nknative.dev/serving/pkg/queue.(*appRequestMetricsHandler).ServeHTTP\n\tknative.dev/serving/pkg/queue/request_metric.go:199\nknative.dev/serving/pkg/queue/sharedmain.mainHandler.ProxyHandler.func3\n\tknative.dev/serving/pkg/queue/handler.go:76\nnet/http.HandlerFunc.ServeHTTP\n\tnet/http/server.go:2136\nknative.dev/serving/pkg/queue/sharedmain.mainHandler.ForwardedShimHandler.func4\n\tknative.dev/serving/pkg/queue/forwarded_shim.go:54\nnet/http.HandlerFunc.ServeHTTP\n\tnet/http/server.go:2136\nknative.dev/serving/pkg/http/handler.(*timeoutHandler).ServeHTTP.func4\n\tknative.dev/serving/pkg/http/handler/timeout.go:118"}
queue-proxy {"httpRequest": {"requestMethod": "GET", "requestUrl": "/", "requestSize": "0", "status": 502, "responseSize": "53", "userAgent": "curl/8.4.0", "remoteIp": "10.42.0.20:52782", "serverIp": "10.42.0.34", "referer": "", "latency": "0.001448584s", "protocol": "HTTP/1.1"}, "traceId": "]"}
queue-proxy {"httpRequest": {"requestMethod": "GET", "requestUrl": "/", "requestSize": "0", "status": 502, "responseSize": "53", "userAgent": "curl/8.4.0", "remoteIp": "10.42.0.20:52782", "serverIp": "10.42.0.34", "referer": "", "latency": "0.000335542s", "protocol": "HTTP/1.1"}, "traceId": "]"}
queue-proxy {"severity":"ERROR","timestamp":"2024-01-18T14:36:06.781777885Z","logger":"queueproxy","caller":"network/error_handler.go:33","message":"error reverse proxying request; sockstat: sockets: used 8\nTCP: inuse 0 orphan 0 tw 15 alloc 185 mem 0\nUDP: inuse 0 mem 0\nUDPLITE: inuse 0\nRAW: inuse 0\nFRAG: inuse 0 memory 0\n","commit":"d96dabb-dirty","knative.dev/key":"default/runtime-00001","knative.dev/pod":"runtime-00001-deployment-7b9c49d676-dlxmt","error":"dial tcp 127.0.0.1:8080: connect: connection refused","stacktrace":"knative.dev/pkg/network.ErrorHandler.func1\n\tknative.dev/pkg@v0.0.0-20240115132401-f95090a164db/network/error_handler.go:33\nnet/http/httputil.(*ReverseProxy).ServeHTTP\n\tnet/http/httputil/reverseproxy.go:475\nknative.dev/serving/pkg/queue.(*appRequestMetricsHandler).ServeHTTP\n\tknative.dev/serving/pkg/queue/request_metric.go:199\nknative.dev/serving/pkg/queue/sharedmain.mainHandler.ProxyHandler.func3\n\tknative.dev/serving/pkg/queue/handler.go:76\nnet/http.HandlerFunc.ServeHTTP\n\tnet/http/server.go:2136\nknative.dev/serving/pkg/queue/sharedmain.mainHandler.ForwardedShimHandler.func4\n\tknative.dev/serving/pkg/queue/forwarded_shim.go:54\nnet/http.HandlerFunc.ServeHTTP\n\tnet/http/server.go:2136\nknative.dev/serving/pkg/http/handler.(*timeoutHandler).ServeHTTP.func4\n\tknative.dev/serving/pkg/http/handler/timeout.go:118"}
queue-proxy {"httpRequest": {"requestMethod": "GET", "requestUrl": "/", "requestSize": "0", "status": 502, "responseSize": "53", "userAgent": "curl/8.4.0", "remoteIp": "10.42.0.20:52782", "serverIp": "10.42.0.34", "referer": "", "latency": "0.00029575s", "protocol": "HTTP/1.1"}, "traceId": "]"}
queue-proxy {"severity":"ERROR","timestamp":"2024-01-18T14:36:07.815057125Z","logger":"queueproxy","caller":"network/error_handler.go:33","message":"error reverse proxying request; sockstat: sockets: used 8\nTCP: inuse 0 orphan 0 tw 15 alloc 185 mem 0\nUDP: inuse 0 mem 0\nUDPLITE: inuse 0\nRAW: inuse 0\nFRAG: inuse 0 memory 0\n","commit":"d96dabb-dirty","knative.dev/key":"default/runtime-00001","knative.dev/pod":"runtime-00001-deployment-7b9c49d676-dlxmt","error":"dial tcp 127.0.0.1:8080: connect: connection refused","stacktrace":"knative.dev/pkg/network.ErrorHandler.func1\n\tknative.dev/pkg@v0.0.0-20240115132401-f95090a164db/network/error_handler.go:33\nnet/http/httputil.(*ReverseProxy).ServeHTTP\n\tnet/http/httputil/reverseproxy.go:475\nknative.dev/serving/pkg/queue.(*appRequestMetricsHandler).ServeHTTP\n\tknative.dev/serving/pkg/queue/request_metric.go:199\nknative.dev/serving/pkg/queue/sharedmain.mainHandler.ProxyHandler.func3\n\tknative.dev/serving/pkg/queue/handler.go:76\nnet/http.HandlerFunc.ServeHTTP\n\tnet/http/server.go:2136\nknative.dev/serving/pkg/queue/sharedmain.mainHandler.ForwardedShimHandler.func4\n\tknative.dev/serving/pkg/queue/forwarded_shim.go:54\nnet/http.HandlerFunc.ServeHTTP\n\tnet/http/server.go:2136\nknative.dev/serving/pkg/http/handler.(*timeoutHandler).ServeHTTP.func4\n\tknative.dev/serving/pkg/http/handler/timeout.go:118"}
queue-proxy {"httpRequest": {"requestMethod": "GET", "requestUrl": "/", "requestSize": "0", "status": 502, "responseSize": "53", "userAgent": "curl/8.4.0", "remoteIp": "10.42.0.20:52782", "serverIp": "10.42.0.34", "referer": "", "latency": "0.000280208s", "protocol": "HTTP/1.1"}, "traceId": "]"}
queue-proxy {"severity":"ERROR","timestamp":"2024-01-18T14:36:08.847556037Z","logger":"queueproxy","caller":"network/error_handler.go:33","message":"error reverse proxying request; sockstat: sockets: used 8\nTCP: inuse 0 orphan 0 tw 15 alloc 185 mem 0\nUDP: inuse 0 mem 0\nUDPLITE: inuse 0\nRAW: inuse 0\nFRAG: inuse 0 memory 0\n","commit":"d96dabb-dirty","knative.dev/key":"default/runtime-00001","knative.dev/pod":"runtime-00001-deployment-7b9c49d676-dlxmt","error":"dial tcp 127.0.0.1:8080: connect: connection refused","stacktrace":"knative.dev/pkg/network.ErrorHandler.func1\n\tknative.dev/pkg@v0.0.0-20240115132401-f95090a164db/network/error_handler.go:33\nnet/http/httputil.(*ReverseProxy).ServeHTTP\n\tnet/http/httputil/reverseproxy.go:475\nknative.dev/serving/pkg/queue.(*appRequestMetricsHandler).ServeHTTP\n\tknative.dev/serving/pkg/queue/request_metric.go:199\nknative.dev/serving/pkg/queue/sharedmain.mainHandler.ProxyHandler.func3\n\tknative.dev/serving/pkg/queue/handler.go:76\nnet/http.HandlerFunc.ServeHTTP\n\tnet/http/server.go:2136\nknative.dev/serving/pkg/queue/sharedmain.mainHandler.ForwardedShimHandler.func4\n\tknative.dev/serving/pkg/queue/forwarded_shim.go:54\nnet/http.HandlerFunc.ServeHTTP\n\tnet/http/server.go:2136\nknative.dev/serving/pkg/http/handler.(*timeoutHandler).ServeHTTP.func4\n\tknative.dev/serving/pkg/http/handler/timeout.go:118"}
queue-proxy {"severity":"ERROR","timestamp":"2024-01-18T14:36:09.897340832Z","logger":"queueproxy","caller":"network/error_handler.go:33","message":"error reverse proxying request; sockstat: sockets: used 8\nTCP: inuse 0 orphan 0 tw 15 alloc 185 mem 0\nUDP: inuse 0 mem 0\nUDPLITE: inuse 0\nRAW: inuse 0\nFRAG: inuse 0 memory 0\n","commit":"d96dabb-dirty","knative.dev/key":"default/runtime-00001","knative.dev/pod":"runtime-00001-deployment-7b9c49d676-dlxmt","error":"dial tcp 127.0.0.1:8080: connect: connection refused","stacktrace":"knative.dev/pkg/network.ErrorHandler.func1\n\tknative.dev/pkg@v0.0.0-20240115132401-f95090a164db/network/error_handler.go:33\nnet/http/httputil.(*ReverseProxy).ServeHTTP\n\tnet/http/httputil/reverseproxy.go:475\nknative.dev/serving/pkg/queue.(*appRequestMetricsHandler).ServeHTTP\n\tknative.dev/serving/pkg/queue/request_metric.go:199\nknative.dev/serving/pkg/queue/sharedmain.mainHandler.ProxyHandler.func3\n\tknative.dev/serving/pkg/queue/handler.go:76\nnet/http.HandlerFunc.ServeHTTP\n\tnet/http/server.go:2136\nknative.dev/serving/pkg/queue/sharedmain.mainHandler.ForwardedShimHandler.func4\n\tknative.dev/serving/pkg/queue/forwarded_shim.go:54\nnet/http.HandlerFunc.ServeHTTP\n\tnet/http/server.go:2136\nknative.dev/serving/pkg/http/handler.(*timeoutHandler).ServeHTTP.func4\n\tknative.dev/serving/pkg/http/handler/timeout.go:118"}

# The user sees
* Connection #0 to host runtime.default.172.17.0.100.sslip.io left intact
*   Trying 172.17.0.100:80...
* Connected to runtime.default.172.17.0.100.sslip.io (172.17.0.100) port 80
> GET / HTTP/1.1
> Host: runtime.default.172.17.0.100.sslip.io
> User-Agent: curl/8.4.0
> Accept: */*
>
< HTTP/1.1 502 Bad Gateway
HTTP/1.1 502 Bad Gateway
< content-length: 53
content-length: 53
< content-type: text/plain; charset=utf-8
content-type: text/plain; charset=utf-8
< date: Thu, 18 Jan 2024 14:36:09 GMT
date: Thu, 18 Jan 2024 14:36:09 GMT
< x-content-type-options: nosniff
x-content-type-options: nosniff
< x-envoy-upstream-service-time: 1
x-envoy-upstream-service-time: 1
< server: envoy
server: envoy

<
dial tcp 127.0.0.1:8080: connect: connection refused
```

#### Summary
* Exec liveness probes do have a race-condition
* As Queue-Proxy is not aware of the (restarting) state of the User-Container, it tries to send traffic to a closed socket
* For a short period of time, this causes errors to be propagated to the caller outside the system
* This can be omitted, when the readiness probe fails at the same time as the liveness probe, then traffic is removed during restart 


## Testing LivenessProbes with multiple containers

* Disable the webhook validation upfront

```bash
kubectl apply -f 2-multi-container/2-ksvc-default-liveness.yaml
```

```bash
ko apply -f 2-multi-container/4-ksvc-liveness-toggle.yaml

# toggle the main containers liveness to false
curl  -iv http://test-probe.default.172.17.0.100.sslip.io/toggleLive

# Check the Queue-Proxys readiness probe
kubectl exec deployment/curl -n default -it -- curl -iv http://10.42.0.18:8012 -H "K-Network-Probe: queue" -H "K-Kubelet-Probe: value"
HTTP/1.1 200 OK

# K8s will restart the first container, but Knative will not know about this
Stream closed EOF for default/test-probe-00001-deployment-78cbfd5cb6-tmsfm (first-container)
queue-proxy {"severity":"ERROR","timestamp":"2024-01-19T07:10:12.650384688Z","logger":"queueproxy","caller":"network/error_handler.go:33","message":"error reverse proxying request; sockstat: sockets: used 9\nTCP: inuse 0 orphan 0 tw 25 alloc 168 mem 0\nUDP: inuse 0 mem 256\nUDPLITE: inuse 0\nRAW: inuse 0\nFRAG: inuse 0 memory 0\n","commit":"d96dabb-dirty","knative.dev/key":"default/test-probe-00001","knative.dev/pod":"test-probe-00001-deployment-78cbfd5cb6-tmsfm","error":"dial tcp 127.0.0.1:8080: connect: connection refused","stacktrace":"knative.dev/pkg/network.ErrorHandler.func1\n\tknative.dev/pkg@v0.0.0-20240115132401-f95090a164db/network/error_handler.go:33\nnet/http/httputil.(*ReverseProxy).ServeHTTP\n\tnet/http/httputil/reverseproxy.go:475\nknative.dev/serving/pkg/queue.(*appRequestMetricsHandler).ServeHTTP\n\tknative.dev/serving/pkg/queue/request_metric.go:199\nknative.dev/serving/pkg/queue/sharedmain.mainHandler.ProxyHandler.func3\n\tknative.dev/serving/pkg/queue/handler.go:76\nnet/http.HandlerFunc.ServeHTTP\n\tnet/http/server.go:2136\nknative.dev/serving/pkg/queue/sharedmain.mainHandler.ForwardedShimHandler.func4\n\tknative.dev/serving/pkg/queue/forwarded_shim.go:54\nnet/http.HandlerFunc.ServeHTTP\n\tnet/http/server.go:2136\nknative.dev/serving/pkg/http/handler.(*timeoutHandler).ServeHTTP.func4\n\tknative.dev/serving/pkg/http/handler/timeout.go:118"}
queue-proxy {"httpRequest": {"requestMethod": "GET", "requestUrl": "/", "requestSize": "0", "status": 502, "responseSize": "53", "userAgent": "curl/8.4.0", "remoteIp": "10.42.0.20:52686", "serverIp": "10.42.0.36", "referer": "", "latency": "0.000348248s", "protocol": "HTTP/1.1"}, "traceId": "]"}
queue-proxy {"severity":"ERROR","timestamp":"2024-01-19T07:10:13.67737702Z","logger":"queueproxy","caller":"network/error_handler.go:33","message":"error reverse proxying request; sockstat: sockets: used 9\nTCP: inuse 0 orphan 0 tw 25 alloc 168 mem 0\nUDP: inuse 0 mem 256\nUDPLITE: inuse 0\nRAW: inuse 0\nFRAG: inuse 0 memory 0\n","commit":"d96dabb-dirty","knative.dev/key":"default/test-probe-00001","knative.dev/pod":"test-probe-00001-deployment-78cbfd5cb6-tmsfm","error":"dial tcp 127.0.0.1:8080: connect: connection refused","stacktrace":"knative.dev/pkg/network.ErrorHandler.func1\n\tknative.dev/pkg@v0.0.0-20240115132401-f95090a164db/network/error_handler.go:33\nnet/http/httputil.(*ReverseProxy).ServeHTTP\n\tnet/http/httputil/reverseproxy.go:475\nknative.dev/serving/pkg/queue.(*appRequestMetricsHandler).ServeHTTP\n\tknative.dev/serving/pkg/queue/request_metric.go:199\nknative.dev/serving/pkg/queue/sharedmain.mainHandler.ProxyHandler.func3\n\tknative.dev/serving/pkg/queue/handler.go:76\nnet/http.HandlerFunc.ServeHTTP\n\tnet/http/server.go:2136\nknative.dev/serving/pkg/queue/sharedmain.mainHandler.ForwardedShimHandler.func4\n\tknative.dev/serving/pkg/queue/forwarded_shim.go:54\nnet/http.HandlerFunc.ServeHTTP\n\tnet/http/server.go:2136\nknative.dev/serving/pkg/http/handler.(*timeoutHandler).ServeHTTP.func4\n\tknative.dev/serving/pkg/http/handler/timeout.go:118"}
queue-proxy {"httpRequest": {"requestMethod": "GET", "requestUrl": "/", "requestSize": "0", "status": 502, "responseSize": "53", "userAgent": "curl/8.4.0", "remoteIp": "10.42.0.20:52686", "serverIp": "10.42.0.36", "referer": "", "latency": "0.000290081s", "protocol": "HTTP/1.1"}, "traceId": "]"}
queue-proxy {"severity":"ERROR","timestamp":"2024-01-19T07:10:14.726464988Z","logger":"queueproxy","caller":"network/error_handler.go:33","message":"error reverse proxying request; sockstat: sockets: used 9\nTCP: inuse 0 orphan 0 tw 25 alloc 168 mem 0\nUDP: inuse 0 mem 256\nUDPLITE: inuse 0\nRAW: inuse 0\nFRAG: inuse 0 memory 0\n","commit":"d96dabb-dirty","knative.dev/key":"default/test-probe-00001","knative.dev/pod":"test-probe-00001-deployment-78cbfd5cb6-tmsfm","error":"dial tcp 127.0.0.1:8080: connect: connection refused","stacktrace":"knative.dev/pkg/network.ErrorHandler.func1\n\tknative.dev/pkg@v0.0.0-20240115132401-f95090a164db/network/error_handler.go:33\nnet/http/httputil.(*ReverseProxy).ServeHTTP\n\tnet/http/httputil/reverseproxy.go:475\nknative.dev/serving/pkg/queue.(*appRequestMetricsHandler).ServeHTTP\n\tknative.dev/serving/pkg/queue/request_metric.go:199\nknative.dev/serving/pkg/queue/sharedmain.mainHandler.ProxyHandler.func3\n\tknative.dev/serving/pkg/queue/handler.go:76\nnet/http.HandlerFunc.ServeHTTP\n\tnet/http/server.go:2136\nknative.dev/serving/pkg/queue/sharedmain.mainHandler.ForwardedShimHandler.func4\n\tknative.dev/serving/pkg/queue/forwarded_shim.go:54\nnet/http.HandlerFunc.ServeHTTP\n\tnet/http/server.go:2136\nknative.dev/serving/pkg/http/handler.(*timeoutHandler).ServeHTTP.func4\n\tknative.dev/serving/pkg/http/handler/timeout.go:118"}
queue-proxy {"httpRequest": {"requestMethod": "GET", "requestUrl": "/", "requestSize": "0", "status": 502, "responseSize": "53", "userAgent": "curl/8.4.0", "remoteIp": "10.42.0.20:52686", "serverIp": "10.42.0.36", "referer": "", "latency": "0.000768828s", "protocol": "HTTP/1.1"}, "traceId": "]"}
queue-proxy {"severity":"ERROR","timestamp":"2024-01-19T07:10:15.766776681Z","logger":"queueproxy","caller":"network/error_handler.go:33","message":"error reverse proxying request; sockstat: sockets: used 9\nTCP: inuse 0 orphan 0 tw 25 alloc 168 mem 0\nUDP: inuse 0 mem 256\nUDPLITE: inuse 0\nRAW: inuse 0\nFRAG: inuse 0 memory 0\n","commit":"d96dabb-dirty","knative.dev/key":"default/test-probe-00001","knative.dev/pod":"test-probe-00001-deployment-78cbfd5cb6-tmsfm","error":"dial tcp 127.0.0.1:8080: connect: connection refused","stacktrace":"knative.dev/pkg/network.ErrorHandler.func1\n\tknative.dev/pkg@v0.0.0-20240115132401-f95090a164db/network/error_handler.go:33\nnet/http/httputil.(*ReverseProxy).ServeHTTP\n\tnet/http/httputil/reverseproxy.go:475\nknative.dev/serving/pkg/queue.(*appRequestMetricsHandler).ServeHTTP\n\tknative.dev/serving/pkg/queue/request_metric.go:199\nknative.dev/serving/pkg/queue/sharedmain.mainHandler.ProxyHandler.func3\n\tknative.dev/serving/pkg/queue/handler.go:76\nnet/http.HandlerFunc.ServeHTTP\n\tnet/http/server.go:2136\nknative.dev/serving/pkg/queue/sharedmain.mainHandler.ForwardedShimHandler.func4\n\tknative.dev/serving/pkg/queue/forwarded_shim.go:54\nnet/http.HandlerFunc.ServeHTTP\n\tnet/http/server.go:2136\nknative.dev/serving/pkg/http/handler.(*timeoutHandler).ServeHTTP.func4\n\tknative.dev/serving/pkg/http/handler/timeout.go:118"}
queue-proxy {"httpRequest": {"requestMethod": "GET", "requestUrl": "/", "requestSize": "0", "status": 502, "responseSize": "53", "userAgent": "curl/8.4.0", "remoteIp": "10.42.0.20:52686", "serverIp": "10.42.0.36", "referer": "", "latency": "0.000718329s", "protocol": "HTTP/1.1"}, "traceId": "]"}
queue-proxy {"severity":"ERROR","timestamp":"2024-01-19T07:10:16.807792449Z","logger":"queueproxy","caller":"network/error_handler.go:33","message":"error reverse proxying request; sockstat: sockets: used 9\nTCP: inuse 0 orphan 0 tw 25 alloc 168 mem 0\nUDP: inuse 0 mem 256\nUDPLITE: inuse 0\nRAW: inuse 0\nFRAG: inuse 0 memory 0\n","commit":"d96dabb-dirty","knative.dev/key":"default/test-probe-00001","knative.dev/pod":"test-probe-00001-deployment-78cbfd5cb6-tmsfm","error":"dial tcp 127.0.0.1:8080: connect: connection refused","stacktrace":"knative.dev/pkg/network.ErrorHandler.func1\n\tknative.dev/pkg@v0.0.0-20240115132401-f95090a164db/network/error_handler.go:33\nnet/http/httputil.(*ReverseProxy).ServeHTTP\n\tnet/http/httputil/reverseproxy.go:475\nknative.dev/serving/pkg/queue.(*appRequestMetricsHandler).ServeHTTP\n\tknative.dev/serving/pkg/queue/request_metric.go:199\nknative.dev/serving/pkg/queue/sharedmain.mainHandler.ProxyHandler.func3\n\tknative.dev/serving/pkg/queue/handler.go:76\nnet/http.HandlerFunc.ServeHTTP\n\tnet/http/server.go:2136\nknative.dev/serving/pkg/queue/sharedmain.mainHandler.ForwardedShimHandler.func4\n\tknative.dev/serving/pkg/queue/forwarded_shim.go:54\nnet/http.HandlerFunc.ServeHTTP\n\tnet/http/server.go:2136\nknative.dev/serving/pkg/http/handler.(*timeoutHandler).ServeHTTP.func4\n\tknative.dev/serving/pkg/http/handler/timeout.go:118"}
queue-proxy {"httpRequest": {"requestMethod": "GET", "requestUrl": "/", "requestSize": "0", "status": 502, "responseSize": "53", "userAgent": "curl/8.4.0", "remoteIp": "10.42.0.20:52686", "serverIp": "10.42.0.36", "referer": "", "latency": "0.000729954s", "protocol": "HTTP/1.1"}, "traceId": "]"}

# Depending on the timing, it is possible that errors propagate to the caller:
* Connection #0 to host test-probe.default.172.17.0.100.sslip.io left intact
*   Trying 172.17.0.100:80...
* Connected to test-probe.default.172.17.0.100.sslip.io (172.17.0.100) port 80
< HTTP/1.1 502 Bad Gateway
HTTP/1.1 502 Bad Gateway
< server: envoy
server: envoy
>
dial tcp 127.0.0.1:8080: connect: connection refused
```

The same test with the liveness-probe on the second container

```bash
# use a curl pod to deactivate different probes
kubectl exec deployment/curl -n default -it -- curl -iv http://<pod-ip>:8090/toggleLive

# Logs
# K8s will restart the sidecar container
second-container Liveness probe called, responding with:  false
first-container Liveness probe called, responding with:  true
second-container Liveness probe called, responding with:  false
first-container Liveness probe called, responding with:  true
queue-proxy {"severity":"INFO","timestamp":"2024-01-19T07:12:41.367340204Z","logger":"queueproxy","caller":"sharedmain/handlers.go:107","message":"Attached drain handler from user-container&{GET /wait-for-drain HTTP/1.1 1 1 map[Accept:[*/*] Accept-Encoding:[gzip] User-Agent:[kube-lifecycle/1.28]] {} <nil> 0 ] false 10.42.0.36:8022 map] map] <nil> map] 10.42.0.1:40470 /wait-for-drain <nil> <nil> <nil> 0x4000467130}","commit":"d96dabb-dirty","knative.dev/key":"default/test-probe-00001","knative.dev/pod":"test-probe-00001-deployment-78cbfd5cb6-tmsfm"}
Stream closed EOF for default/test-probe-00001-deployment-78cbfd5cb6-tmsfm (second-container)
first-container Liveness probe called, responding with:  true
first-container Liveness probe called, responding with:  true

# Now it depends on what the sidecar actually does. If it is important for the request-path, users could see errors as well
# We also seem to have an issue here, K8s attaches the wait-for-drain hook, but our pod is immediately terminated anyway.
```

### Summary

* Liveness probes can not be enabled without interference. We need to
  * Add the additional header also for additional probes
  * Investigate why the wait-for-drain hook is not working (or not holding the SIGTERM long enough)
* Also. there can be race conditions:
  * Queue-Proxy (and other Knative components) will not know about the UC being not live and being restarted. We'll see `HTTP/1.1 503 Service Unavailable` when calling the Knative Service
  * The same applies for sidecars. Queue-Proxy (and other Knative components) will not know about this. Depending on what the sidecar does, this can cause issues
  * Users are required to also fail ReadinessProbes to make sure traffic is removed from a restarting service. Note: there is still no timing guarantees here, but this is the same issue with vanilla K8s workload


## Testing ReadinessProbes with multiple containers

```bash
ko apply -f 2-multi-container/5-ksvc-readiness-toggle.yaml

# toggle readiness in main container
kubectl exec deployment/curl -n default -it -- curl -iv http://10.42.0.37:8080/toggleReady

# Knative Service is not ready, as we are waiting for Endpoints
k get ksvc
NAME         URL                                               LATESTCREATED      LATESTREADY   READY     REASON
test-probe   http://test-probe.default.172.17.0.100.sslip.io   test-probe-00001                 Unknown   RevisionMissing

k get configuration
NAME         LATESTCREATED      LATESTREADY   READY     REASON
test-probe   test-probe-00001                 Unknown

k get king
No resources found in default namespace.

# Knative Service returns a 404
curl -iv http://test-probe.default.172.17.0.100.sslip.io
HTTP/1.1 404 Not Found

# set the second container to ready
kubectl exec deployment/curl -n default -it -- curl -iv http://10.42.0.37:8090/toggleReady

# every thing works now as expected

# set the first container to not ready
kubectl exec deployment/curl -n default -it -- curl -iv http://10.42.0.37:8080/toggleReady

# QP knows about it and starts polling again
queue-proxy context deadline exceeded
first-container Readiness probe called, responding with:  false
first-container Readiness probe called, responding with:  false

# QPs own readiness-probe starts to fail:
kubectl exec deployment/curl -n default -it -- curl -iv http://10.42.0.37:8012 -H "K-Network-Probe: queue" -H "K-Kubelet-Probe: value"
HTTP/1.1 503 Service Unavailable

# Knative will only reflect the error in the SKS
k get configuration,ksvc,king,sks

NAME                                           LATESTCREATED      LATESTREADY        READY   REASON
configuration.serving.knative.dev/test-probe   test-probe-00001   test-probe-00001   True

NAME                                     URL                                               LATESTCREATED      LATESTREADY        READY   REASON
service.serving.knative.dev/test-probe   http://test-probe.default.172.17.0.100.sslip.io   test-probe-00001   test-probe-00001   True

NAME                                                 READY   REASON
ingress.networking.internal.knative.dev/test-probe   True

NAME                                                                 MODE    ACTIVATORS   SERVICENAME        PRIVATESERVICENAME         READY     REASON
serverlessservice.networking.internal.knative.dev/test-probe-00001   Proxy   3            test-probe-00001   test-probe-00001-private   Unknown   NoHealthyBackends

# But K8s has removed the endpoints:
k get endpoints -n default test-probe-00001-private
NAME                       ENDPOINTS   AGE
test-probe-00001-private               7m19s

# So we are sending traffic to activator now, who will log requests will be hold there until the timeout is reached OR
# the pod gets ready again 
{"severity":"WARNING","timestamp":"2024-01-16T14:53:44.641328597Z","logger":"activator","caller":"net/revision_backends.go:342","message":"Failed probing pods","commit":"d96dabb-dirty","knative.dev/controller":"activator","knative.dev/pod":"activator-865458fff9-5fgpf","knative.dev/key":"default/test-probe-00001","curDests":{"ready":"","notReady":"10.42.0.18:8012"},"error":"error roundtripping http://10.42.0.18:8012/healthz: context deadline exceeded"}
{"severity":"INFO","timestamp":"2024-01-16T14:53:44.641432431Z","logger":"activator","caller":"net/revision_backends.go:328","message":"Need to reprobe pods who became non-ready","commit":"d96dabb-dirty","knative.dev/controller":"activator","knative.dev/pod":"activator-865458fff9-5fgpf","knative.dev/key":"default/test-probe-00001","IPs":{"keys":"10.42.0.18:8012"}}
{"severity":"INFO","timestamp":"2024-01-16T14:53:44.641752724Z","logger":"activator","caller":"net/throttler.go:331","message":"Updating Revision Throttler with: clusterIP = <nil>, trackers = 0, backends = 0","commit":"d96dabb-dirty","knative.dev/controller":"activator","knative.dev/pod":"activator-865458fff9-5fgpf","knative.dev/key":"default/test-probe-00001"}
{"severity":"INFO","timestamp":"2024-01-16T14:53:44.641780141Z","logger":"activator","caller":"net/throttler.go:323","message":"Set capacity to 0 (backends: 0, index: 0/1)","commit":"d96dabb-dirty","knative.dev/controller":"activator","knative.dev/pod":"activator-865458fff9-5fgpf","knative.dev/key":"default/test-probe-00001"}

# So client request will hang, and eventually timeout (or work again when probes get healthy again).
```

For the same test with the second container we have:

```bash
# do the same as above, until everything is ready and works

# make second container not ready
kubectl exec deployment/curl -n default -it -- curl -iv http://10.42.0.18:8090/toggleReady

# now it gets really good, K8s is removing the endpoint because the Pod is not totally ready
# Traffic will be sent to activator
k get endpoints -n default
NAME                       ENDPOINTS                         AGE
kubernetes                 192.168.5.1:6443                  42d
test-probe-00001           10.42.0.17:8012,10.42.0.17:8112   14m
test-probe-00001-private                                     14m

# Knative will internally not know that something is wrong, but because K8s fails the pod status
# SKS thinkgs there are no healthy backends
k get configuration,ksvc,king,sks

NAME                                           LATESTCREATED      LATESTREADY        READY   REASON
configuration.serving.knative.dev/test-probe   test-probe-00001   test-probe-00001   True

NAME                                     URL                                               LATESTCREATED      LATESTREADY        READY   REASON
service.serving.knative.dev/test-probe   http://test-probe.default.172.17.0.100.sslip.io   test-probe-00001   test-probe-00001   True

NAME                                                 READY   REASON
ingress.networking.internal.knative.dev/test-probe   True

NAME                                                                 MODE    ACTIVATORS   SERVICENAME        PRIVATESERVICENAME         READY     REASON
serverlessservice.networking.internal.knative.dev/test-probe-00001   Proxy   3            test-probe-00001   test-probe-00001-private   Unknown   NoHealthyBackends

# But traffic will still work, which it should not
curl  -iv http://test-probe.default.172.17.0.100.sslip.io
HTTP/1.1 200 OK

# because Activator is only checking QP health, which does not know about the additional container or the pods overall health.
```

### Summary
* Readiness probes on a single container work as expected
* Initially, deployments must become ready at least once to make the `Revision` progress. Without a revision, no `KIngress` is generated. So we can say, we depend on a full K8s initial ready state to progress with KService initialization
* On additional containers, the readiness probes currently do not work, as Knative and ingress layer is not aware of the additional checks
* The state we represent in the SKS is correct, but does not change routing.
* Activator knows where to send requests to, even when endpoints on private service is not populated
  * Activator uses `notReadyAddresses` field in `Endpoints` to do its own probing. If the main probe on QP is ok, activator will forward requests to that pod, even when Kubernetes does not populate it in `Endpoints` and is failing its overall `Pod` readiness.
  * We definitely need to aggregate the readiness of every container in Queue-Proxy to make this consistent
