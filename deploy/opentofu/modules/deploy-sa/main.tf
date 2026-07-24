# The service account GitHub Actions assumes via Workload Identity
# Federation. Lives in its own env project (blast radius); only the
# workloadIdentityUser binding references the shared ops pool.
#
# Least privilege, and asymmetric on purpose:
#   - stg's SA WRITES images (it builds) and READS logs (the smoke gate
#     fetches magic links from Cloud Logging).
#   - pro's SA only READS images (promote-by-digest — pro never builds)
#     and reads no logs (its smoke is /healthz + /login only).

terraform {
  required_providers {
    google = {
      source = "hashicorp/google"
    }
  }
}

resource "google_service_account" "deploy" {
  project      = var.project
  account_id   = "github-deploy"
  display_name = "GitHub Actions deploy"
}

resource "google_project_iam_member" "run_developer" {
  project = var.project
  role    = "roles/run.developer"
  member  = "serviceAccount:${google_service_account.deploy.email}"
}

# Deploying a revision that runs AS the runtime SA requires actAs on it.
resource "google_service_account_iam_member" "act_as_runtime" {
  service_account_id = var.runtime_sa_id
  role               = "roles/iam.serviceAccountUser"
  member             = "serviceAccount:${google_service_account.deploy.email}"
}

resource "google_artifact_registry_repository_iam_member" "registry" {
  project    = var.registry_project
  location   = var.region
  repository = var.registry_repo_id
  role       = var.registry_role
  member     = "serviceAccount:${google_service_account.deploy.email}"
}

resource "google_project_iam_member" "log_reader" {
  count = var.grant_log_reader ? 1 : 0

  project = var.project
  role    = "roles/logging.viewer"
  member  = "serviceAccount:${google_service_account.deploy.email}"
}

# principal_suffix picks WHICH GitHub workloads may assume this SA:
#   "attribute.repository/OWNER/REPO"  (stg: any ref of the repo)
#   "attribute.ref_type/tag"           (pro: tag builds only; the pool
#                                       provider's attribute_condition
#                                       already pinned the repo)
resource "google_service_account_iam_member" "wif" {
  service_account_id = google_service_account.deploy.name
  role               = "roles/iam.workloadIdentityUser"
  member             = "principalSet://iam.googleapis.com/${var.wif_pool_name}/${var.principal_suffix}"
}
