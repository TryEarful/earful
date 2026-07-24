# Secret Manager secrets, two flavors:
#   - generated: values tofu creates (DB DSN, webhook secret) — version
#     managed here.
#   - shells: secrets whose VALUES must never touch tofu state or the
#     repo (BREVO_API_KEY, GOOGLE_CLIENT_SECRET). Tofu creates the empty
#     secret; the operator adds versions out-of-band:
#       printf '%s' "$VALUE" | gcloud secrets versions add NAME \
#         --project PROJECT --data-file=-
#     A shell with no version yet must not be mounted by the service —
#     Cloud Run refuses to start a revision referencing a versionless
#     secret. The env stacks gate those env vars accordingly.
#
# The runtime SA gets accessor per-secret — no project-wide grant.

terraform {
  required_providers {
    google = {
      source = "hashicorp/google"
    }
  }
}

# The map's VALUES are sensitive; its keys are just secret names —
# nonsensitive(keys(...)) says exactly that, and keeps for_each legal.
locals {
  generated_names = toset(nonsensitive(keys(var.generated)))
}

resource "google_secret_manager_secret" "generated" {
  for_each = local.generated_names

  project   = var.project
  secret_id = each.key

  replication {
    auto {}
  }
}

resource "google_secret_manager_secret_version" "generated" {
  for_each = local.generated_names

  secret      = google_secret_manager_secret.generated[each.key].id
  secret_data = var.generated[each.key]
}

resource "google_secret_manager_secret" "shell" {
  for_each = toset(var.shells)

  project   = var.project
  secret_id = each.value

  replication {
    auto {}
  }
}

resource "google_secret_manager_secret_iam_member" "generated_access" {
  for_each = local.generated_names

  project   = var.project
  secret_id = google_secret_manager_secret.generated[each.key].secret_id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${var.accessor_sa_email}"
}

resource "google_secret_manager_secret_iam_member" "shell_access" {
  for_each = toset(var.shells)

  project   = var.project
  secret_id = google_secret_manager_secret.shell[each.value].secret_id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${var.accessor_sa_email}"
}
