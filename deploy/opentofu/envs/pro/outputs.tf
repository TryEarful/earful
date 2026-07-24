output "project_id" {
  value = local.project
}

output "service_url" {
  value = module.app.service_uri
}

output "base_url" {
  value = local.base_url
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

output "backups_bucket" {
  value = module.backups.bucket_name
}

output "email_webhook_path" {
  description = "Paste into Brevo's webhook config (bounces/spam → suppressions)."
  value       = "${local.base_url}/webhooks/email/<EMAIL_WEBHOOK_SECRET value — gcloud secrets versions access latest --secret EMAIL_WEBHOOK_SECRET --project ${local.project}>"
}
