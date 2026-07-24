output "connection_name" {
  value = google_sql_database_instance.main.connection_name
}

# Each instance gets its own service account; bucket grants for
# export/import must target THIS identity, not the project-level Cloud
# SQL service agent (granting the agent 412s with "does not have the
# required permissions for the bucket").
output "service_account_email" {
  value = google_sql_database_instance.main.service_account_email_address
}

output "instance_name" {
  value = google_sql_database_instance.main.name
}

# The complete DSN the app consumes as DATABASE_URL, speaking pgx's URL
# form through the Cloud Run unix-socket mount.
output "database_url" {
  value     = "postgres://${google_sql_user.app.name}:${random_password.db.result}@/${google_sql_database.app.name}?host=/cloudsql/${google_sql_database_instance.main.connection_name}"
  sensitive = true
}
