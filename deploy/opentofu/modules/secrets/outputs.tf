# secret_id => secret_id, for wiring into run-service secret_env.
# depends_on the accessor grants: Cloud Run validates secret access when
# a service/job is updated, so a consumer of these ids must not deploy
# before the runtime SA can actually read the secrets — without this
# edge, adding a new secret races the IAM grant and fails the apply.
output "secret_ids" {
  value = merge(
    { for k, s in google_secret_manager_secret.generated : k => s.secret_id },
    { for k, s in google_secret_manager_secret.shell : k => s.secret_id },
  )

  depends_on = [
    google_secret_manager_secret_iam_member.generated_access,
    google_secret_manager_secret_iam_member.shell_access,
  ]
}
