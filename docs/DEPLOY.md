# Deploy to Google Cloud Run

The system is published on Cloud Run as **two services**: `service-a` (public entry point,
`GET /zipcode/{cep}`) calls `service-b` (ViaCEP + WeatherAPI). Both bind to the `$PORT`
Cloud Run injects.

> Looking for the live demo URLs? They're in the [README](../README.md#deploy-to-google-cloud-run).

## Prerequisites
- `gcloud` CLI authenticated: `gcloud auth login && gcloud config set project <PROJECT_ID>`
- Enable the required APIs:
```bash
gcloud services enable run.googleapis.com cloudbuild.googleapis.com artifactregistry.googleapis.com
```

## One-command deploy
```bash
PROJECT_ID=<your-project> WEATHER_API_KEY=<your-weatherapi-key> ./deploy/cloud-run.sh
```

The script (`deploy/cloud-run.sh`) builds both images via Cloud Build (`cloudbuild.yaml`),
pushes them to Artifact Registry, deploys `service-b` (with `WEATHER_API_KEY`), then deploys
`service-a` wired to the live `service-b` URL via `SERVICE_B_URL`. It prints the public
endpoint at the end. Optional env: `REGION` (default `us-central1`), `REPO` (default `weather`).

> **Tip:** for production, store the key in Secret Manager and pass `--set-secrets` instead of
> `--set-env-vars`. Distributed tracing (OTEL/Zipkin) is local-only via `docker compose`; on
> Cloud Run it is inactive unless you point `OTEL_EXPORTER_OTLP_ENDPOINT` at a reachable collector.