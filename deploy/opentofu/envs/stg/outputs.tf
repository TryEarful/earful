output "project_id" {
  value = local.project
}

output "service_url" {
  value = module.app.service_uri
}

output "base_url" {
  description = "What the app believes its origin is (BASE_URL env)."
  value       = local.base_url
}

output "staging_basic_auth" {
  description = "user:pass for the staging-wide Basic Auth wall; also the value of the STG_BASIC_AUTH GitHub secret."
  value       = "earful:${random_password.basic_auth.result}"
  sensitive   = true
}

output "migrate_command" {
  value = "gcloud run jobs execute ${module.app.migrate_job_name} --project ${local.project} --region ${local.region} --wait"
}

output "deploy_sa_email" {
  value = module.deploy_sa.email
}

output "sql_connection_name" {
  value = module.cloudsql.connection_name
}
