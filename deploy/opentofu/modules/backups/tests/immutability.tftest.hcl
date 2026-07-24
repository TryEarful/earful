# M9-T6 AC: "lifecycle + retention config asserted in opentofu tests".
# Run from modules/backups:  tofu init -backend=false && tofu test
# Mock providers — asserts OUR configuration, no cloud calls.

mock_provider "google" {}
mock_provider "google-beta" {}

variables {
  backups_project       = "earful-backups-test"
  pro_project           = "earful-pro-test"
  region                = "europe-west4"
  sql_instance_name     = "earful"
  sql_instance_sa_email = "p123-abc@gcp-sa-cloud-sql.iam.gserviceaccount.com"
  lock_retention        = true
}

run "immutability_contract" {
  command = plan

  assert {
    condition     = tonumber(google_storage_bucket.exports.retention_policy[0].retention_period) == 2592000
    error_message = "retention must be exactly 30 days (ADR-0008 rolling window)"
  }

  assert {
    condition     = google_storage_bucket.exports.retention_policy[0].is_locked == true
    error_message = "retention policy must LOCK when lock_retention=true — unlocked retention is a suggestion, not a guarantee"
  }

  assert {
    condition = anytrue([
      for r in google_storage_bucket.exports.lifecycle_rule :
      anytrue([for c in r.condition : c.age == 31]) && anytrue([for a in r.action : a.type == "Delete"])
    ])
    error_message = "lifecycle must delete objects at 31 days — one day past retention, so the window rolls itself"
  }

  assert {
    condition     = google_storage_bucket.exports.public_access_prevention == "enforced"
    error_message = "backups bucket must enforce public access prevention"
  }

  assert {
    condition     = google_storage_bucket.exports.uniform_bucket_level_access == true
    error_message = "uniform bucket-level access required — no per-object ACL escape hatch"
  }

  assert {
    condition     = length(google_storage_bucket.exports.versioning) == 0
    error_message = "versioning must stay off — GCS forbids combining it with a retention policy"
  }

  assert {
    condition     = google_storage_bucket_iam_member.sql_instance_writes.role == "roles/storage.objectAdmin"
    error_message = "export writer binding drifted — objectAdmin is the narrowest role the export API accepts; immutability rests on the LOCKED retention policy above, which is what the other asserts pin"
  }
}

run "default_is_unlocked" {
  command = plan

  variables {
    lock_retention = false
  }

  assert {
    condition     = google_storage_bucket.exports.retention_policy[0].is_locked == false
    error_message = "default must be unlocked — the lock flips only after the first restore drill (runbook order)"
  }
}
