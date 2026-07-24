# secret_id => secret_id, for wiring into run-service secret_env.
output "secret_ids" {
  value = merge(
    { for k, s in google_secret_manager_secret.generated : k => s.secret_id },
    { for k, s in google_secret_manager_secret.shell : k => s.secret_id },
  )
}
