# Werte, die GitHub Actions und später der Server brauchen.

output "workload_identity_provider" {
  description = "Für google-github-actions/auth: workload_identity_provider"
  value       = google_iam_workload_identity_pool_provider.github.name
}

output "deploy_dienstkonto" {
  description = "Für google-github-actions/auth: service_account"
  value       = google_service_account.deploy.email
}

output "container_registry" {
  description = "Präfix für Container-Images"
  value       = "${var.region}-docker.pkg.dev/${var.projekt}/${google_artifact_registry_repository.container.repository_id}"
}

output "server_url" {
  description = "Adresse des Servers (für testclient --server)"
  value       = google_cloud_run_v2_service.server.uri
}
