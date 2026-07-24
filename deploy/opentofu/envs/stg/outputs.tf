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

output "migrate_command" {
  value = "gcloud run jobs execute ${module.app.migrate_job_name} --project ${local.project} --region ${local.region} --wait"
}

output "deploy_sa_email" {
  value = module.deploy_sa.email
}

output "sql_connection_name" {
  value = module.cloudsql.connection_name
}
