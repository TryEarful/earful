# The tofu state bucket every root (including this one, post-migration)
# stores state in. Versioned so a bad apply's state can be recovered.
resource "google_storage_bucket" "state" {
  project  = google_project.ops.project_id
  name     = "earful-tofu-state-${var.suffix}"
  location = var.region

  uniform_bucket_level_access = true
  public_access_prevention    = "enforced"

  versioning {
    enabled = true
  }

  # State contains generated DB passwords (documented risk in the plan):
  # nobody but the operator's identity has storage access on ops.

  depends_on = [google_project_service.apis]
}
