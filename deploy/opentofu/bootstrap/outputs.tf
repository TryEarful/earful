output "ops_project_id" {
  value = google_project.ops.project_id
}

output "stg_project_id" {
  value = google_project.stg.project_id
}

output "stg_project_number" {
  value = google_project.stg.number
}

output "pro_project_id" {
  value = google_project.pro.project_id
}

output "pro_project_number" {
  value = google_project.pro.number
}

output "backups_project_id" {
  value = google_project.backups.project_id
}

output "region" {
  value = var.region
}

output "state_bucket" {
  value = google_storage_bucket.state.name
}

output "dns_zone_name" {
  value = google_dns_managed_zone.tryearful.name
}

output "dns_name_servers" {
  description = "Point the Gandi nameservers here — ONLY after the pre-cutover dig-diff gate passes (README)."
  value       = google_dns_managed_zone.tryearful.name_servers
}

output "wif_pool_name" {
  description = "Full pool resource name principalSet bindings hang off."
  value       = google_iam_workload_identity_pool.github.name
}

output "wif_provider_name" {
  description = "What google-github-actions/auth's workload_identity_provider input wants."
  value       = google_iam_workload_identity_pool_provider.github.name
}

output "artifact_registry" {
  description = "Docker prefix for image pushes."
  value       = "${var.region}-docker.pkg.dev/${google_project.ops.project_id}/${google_artifact_registry_repository.earful.repository_id}"
}

output "artifact_registry_repo_id" {
  value = google_artifact_registry_repository.earful.repository_id
}

output "github_repo" {
  value = var.github_repo
}
