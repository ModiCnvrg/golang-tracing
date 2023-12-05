# Installing Coralogix:
# Create secret:
kubectl create secret generic coralogix-keys \
--from-literal=PRIVATE_KEY=<send-your-data-API-key> \
-n monitoring

# Helm
helm repo add coralogix https://cgx.jfrog.io/artifactory/coralogix-charts-virtual
kubectl create namespace monitoring
kubectl create secret generic coralogix-keys --from-literal=PRIVATE_KEY="" -n monitoring
helm upgrade --install otel-coralogix-integration coralogix/otel-integration --render-subchart-notes --create-namespace -n monitoring -f 

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

# Building the image:
docker buildx build --platform linux/amd64 -t modicnvrg/observability-poc:latest .

# Pushing to registry:
docker push modicnvrg/observability-poc:latest


