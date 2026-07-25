# One shared Docker repository: staging deploys push here, production
# promotes the exact digest staging's smoke gate approved — same bytes,
# no rebuild. Cleanup keeps the last 10 images (rollback window) and
# drops anything older than 30 days beyond those.
resource "google_artifact_registry_repository" "earful" {
  project       = google_project.ops.project_id
  location      = var.region
  repository_id = "earful"
  format        = "DOCKER"

  cleanup_policy_dry_run = false

  cleanup_policies {
    id     = "keep-recent"
    action = "KEEP"
    most_recent_versions {
      keep_count = 10
    }
  }

  cleanup_policies {
    id     = "delete-old"
    action = "DELETE"
    condition {
      older_than = "2592000s" # 30 days
    }
  }

  depends_on = [google_project_service.apis]
}

# The identity that builds and pushes images, in ops rather than in an
# environment project.
#
# It used to be staging's deploy account, on the reasoning that staging
# is what builds. That coupling cost us on 2026-07-25: Google suspended
# the staging project, its service accounts went with it, and the
# pipeline could no longer build an image for ANY environment — including
# the production hotfix path, which otherwise shares nothing with
# staging. Building belongs to the registry's own project, which is the
# one place every environment already depends on.
#
# It can push images and do nothing else: no deploy rights, no logs, no
# access to any environment.
resource "google_service_account" "image_builder" {
  project      = google_project.ops.project_id
  account_id   = "github-builder"
  display_name = "GitHub Actions image builder"
}

resource "google_artifact_registry_repository_iam_member" "builder_pushes" {
  project    = google_project.ops.project_id
  location   = var.region
  repository = google_artifact_registry_repository.earful.repository_id
  role       = "roles/artifactregistry.writer"
  member     = "serviceAccount:${google_service_account.image_builder.email}"
}

# Any ref of this repository may build; only tags may deploy to
# production, which stays the deploy accounts' business.
resource "google_service_account_iam_member" "builder_wif" {
  service_account_id = google_service_account.image_builder.name
  role               = "roles/iam.workloadIdentityUser"
  member             = "principalSet://iam.googleapis.com/${google_iam_workload_identity_pool.github.name}/attribute.repository/${var.github_repo}"
}
