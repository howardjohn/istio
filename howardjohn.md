# In place upgrade

```
$ kubectl exec `kp shell` -c istio-proxy -- envoy --version

envoy  version: 29c747cd2d6b7420f96370165ad197bdcdcee2a1/1.16.0-dev/Clean/RELEASE/BoringSSL

$ kubectl exec `kp shell` -c istio-proxy -- curl localhost:15000/server_info -s | jq .version -r
29c747cd2d6b7420f96370165ad197bdcdcee2a1/1.16.0-dev/Clean/RELEASE/BoringSSL
$ cat out/linux_amd64/release/envoy-422ba7d52af4e9e12ae75497f0d7483a565908b1 | kubectl exec -i `kp shell` -c istio-proxy -- sudo sh -c 'cat > /usr/local/bin/envoy-new; chmod +x /usr/local/bin/envoy-new; mv /usr/local/bin/envoy-new /usr/local/bin/envoy'
$ kubectl exec `kp shell` -c istio-proxy -- envoy --version                                                                                                                           
envoy  version: 422ba7d52af4e9e12ae75497f0d7483a565908b1/1.16.0-dev/Clean/RELEASE/BoringSSL

$ kubectl exec `kp shell` -c istio-proxy -- curl localhost:15000/server_info -s | jq .version -r       
422ba7d52af4e9e12ae75497f0d7483a565908b1/1.16.0-dev/Clean/RELEASE/BoringSSL
```