# Installing Coralogix:
# Create secret:
kubectl create secret generic coralogix-keys \
-n monitoring \
--from-literal=PRIVATE_KEY=<send-your-data-API-key>

# Helm
helm repo add coralogix https://cgx.jfrog.io/artifactory/coralogix-charts-virtual
kubectl create secret generic coralogix-keys --from-literal=PRIVATE_KEY="cxtp_oxXp9dX3lPQ4vY3qM4HlqH7eZowc37"
helm upgrade --install otel-coralogix-integration coralogix/otel-integration --render-subchart-notes -f 

# Uninstall
helm uninstall otel-coralogix-integration

# Tunneling to a service
minikube service tracing-poc-service

# Load image into minikube:
minikube image load tracing-poc

# Install Prometheus operator:
helm upgrade --install prometheus-coralogix coralogix-charts-virtual/prometheus-operator-coralogix \
-f prom-override.yaml \
--namespace=monitoring


