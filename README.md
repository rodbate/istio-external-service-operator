Istio External Service Operator
===

```yaml
apiVersion: istio.rodbate.github.com/v1
kind: IstioExternalService
metadata:
  name: istioexternalservice-sample
spec:
  services:
    - name: ex-service-1
      endpoints:
        - host: www.google.com
          port: 80
          healthCheck:
            periodSeconds: 4
        - host: 151.101.129.69
          port: 80
    - name: ex-service-2
      endpoints:
        - host: stackoverflow.com
          port: 80
          healthCheck:
            timeoutSeconds: 2
            periodSeconds: 3
            successThreshold: 1
            failureThreshold: 5

```
