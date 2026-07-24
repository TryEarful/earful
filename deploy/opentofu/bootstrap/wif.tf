# GitHub Actions → GCP with no static keys: the deploy workflow presents
# its GitHub OIDC token, restricted here to this one repository. The
# per-environment deploy service accounts (created by the env stacks) bind
# themselves to principalSets on this pool:
#   - stg:  attribute.repository/<repo>       (any ref may deploy staging)
#   - pro:  attribute.ref_type/tag            (only tag builds may deploy pro)
# The pro principalSet not mentioning the repo is safe because the
# provider-level attribute_condition already rejects every other repo's
# tokens before attributes are even mapped.
resource "google_iam_workload_identity_pool" "github" {
  project                   = google_project.ops.project_id
  workload_identity_pool_id = "github"
  display_name              = "GitHub Actions"

  depends_on = [google_project_service.apis]
}

resource "google_iam_workload_identity_pool_provider" "github" {
  project                            = google_project.ops.project_id
  workload_identity_pool_id          = google_iam_workload_identity_pool.github.workload_identity_pool_id
  workload_identity_pool_provider_id = "github-oidc"
  display_name                       = "GitHub OIDC"

  attribute_mapping = {
    "google.subject"       = "assertion.sub"
    "attribute.repository" = "assertion.repository"
    "attribute.ref"        = "assertion.ref"
    "attribute.ref_type"   = "assertion.ref.startsWith('refs/tags/') ? 'tag' : 'branch'"
  }

  attribute_condition = "assertion.repository == \"${var.github_repo}\""

  oidc {
    issuer_uri = "https://token.actions.githubusercontent.com"
  }
}
