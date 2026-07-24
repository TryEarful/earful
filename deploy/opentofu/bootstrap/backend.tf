# Uncommented after the first apply created the bucket; bootstrap's state
# now lives beside the env states (see README step 2). The bucket name is
# supplied at init time via -backend-config (like envs/stg and envs/pro)
# rather than hardcoded, so the real state-bucket name stays out of this
# public repo.
terraform {
  backend "gcs" {
    prefix = "bootstrap"
  }
}
