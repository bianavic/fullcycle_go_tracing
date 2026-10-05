#!/usr/bin/env bash
#
# Deploy the Weather-by-Zipcode system to Google Cloud Run.
#
# Deploys two Cloud Run services:
#   - service-b: city lookup (ViaCEP) + temperature (WeatherAPI)
#   - service-a: public entry point POST /weather, calls service-b
#
# Prerequisites:
#   - gcloud CLI authenticated: gcloud auth login && gcloud config set project <id>
#   - APIs enabled: run.googleapis.com, cloudbuild.googleapis.com, artifactregistry.googleapis.com
#
# Usage:
#   PROJECT_ID=my-proj WEATHER_API_KEY=xxxxx ./deploy/cloud-run.sh
#
# Optional env: REGION (default us-central1), REPO (default weather)

set -euo pipefail

PROJECT_ID="${PROJECT_ID:?set PROJECT_ID (e.g. PROJECT_ID=my-proj)}"
WEATHER_API_KEY="${WEATHER_API_KEY:?set WEATHER_API_KEY (your WeatherAPI.com key)}"
REGION="${REGION:-us-central1}"
REPO="${REPO:-weather}"

IMG_BASE="${REGION}-docker.pkg.dev/${PROJECT_ID}/${REPO}"

echo "==> Project=${PROJECT_ID}  Region=${REGION}  Repo=${REPO}"

# 0. Ensure the Artifact Registry repo exists (no-op if it already does).
echo "==> Ensuring Artifact Registry repo '${REPO}' exists"
gcloud artifacts repositories create "${REPO}" \
  --repository-format=docker --location="${REGION}" \
  --description="Weather by Zipcode images" 2>/dev/null || true

# 1. Build & push both images via Cloud Build.
for svc in service-a service-b; do
  echo "==> Building & pushing ${svc}"
  gcloud builds submit --config cloudbuild.yaml \
    --substitutions="_SERVICE=${svc},_REGION=${REGION},_REPO=${REPO}"
done

# 2. Deploy service-b first (it needs the WeatherAPI key).
echo "==> Deploying service-b"
gcloud run deploy service-b \
  --image "${IMG_BASE}/service-b:latest" \
  --region "${REGION}" \
  --platform managed \
  --allow-unauthenticated \
  --set-env-vars "WEATHER_API_KEY=${WEATHER_API_KEY}"

SERVICE_B_URL="$(gcloud run services describe service-b \
  --region "${REGION}" --format='value(status.url)')"
echo "==> service-b URL: ${SERVICE_B_URL}"

# 3. Deploy service-a (public entry point), pointing it at service-b.
echo "==> Deploying service-a"
gcloud run deploy service-a \
  --image "${IMG_BASE}/service-a:latest" \
  --region "${REGION}" \
  --platform managed \
  --allow-unauthenticated \
  --set-env-vars "SERVICE_B_URL=${SERVICE_B_URL}"

SERVICE_A_URL="$(gcloud run services describe service-a \
  --region "${REGION}" --format='value(status.url)')"

echo ""
echo "==> Done. Public endpoint:"
echo "    curl -X POST ${SERVICE_A_URL}/weather -H 'Content-Type: application/json' -d '{\"cep\":\"01001000\"}'"