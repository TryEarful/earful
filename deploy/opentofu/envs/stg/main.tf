# Staging: the walking skeleton (M1). Everything the pipeline touches
# lives here; everything shared (projects, DNS, registry, WIF) is read
# from bootstrap's remote state — no hand-copied IDs to drift.
#
# init (backend is partial because the bucket name embeds the suffix):
#   tofu init \
#     -backend-config="bucket=earful-tofu-state-<sfx>" \
#     -backend-config="prefix=stg"

terraform {
  required_version = ">= 1.6"

  backend "gcs" {}

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 6.0"
    }
    google-beta = {
      source  = "hashicorp/google-beta"
      version = "~> 6.0"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.6"
    }
  }
}

data "terraform_remote_state" "bootstrap" {
  backend = "gcs"
  config = {
    bucket = var.state_bucket
    prefix = "bootstrap"
  }
}

locals {
  boot    = data.terraform_remote_state.bootstrap.outputs
  project = local.boot.stg_project_id
  region  = local.boot.region

  # Cloud Run's deterministic URL — knowable before the service exists,
  # which is what lets BASE_URL be set in the same apply that creates the
  # service. Post-cutover, custom_domain overrides it.
  run_app_host = "earful-${local.boot.stg_project_number}.${local.region}.run.app"
  host         = var.custom_domain != "" ? var.custom_domain : local.run_app_host
  base_url     = "https://${local.host}"

  seed_image = "${local.boot.artifact_registry}/earful:seed"
}

# user_project_override: some APIs (orgpolicy among them) refuse plain
# user ADC without an explicit quota project; route those through this
# env's own project.
provider "google" {
  region                = local.boot.region
  billing_project       = local.boot.stg_project_id
  user_project_override = true
}

provider "google-beta" {
  region                = local.boot.region
  billing_project       = local.boot.stg_project_id
  user_project_override = true
}

# Vertex is called with the service's own identity — no key, nothing to
# leak (see envs/pro for the same grant on production).
resource "google_project_iam_member" "runtime_vertex" {
  project = local.project
  role    = "roles/aiplatform.user"
  member  = "serviceAccount:${google_service_account.runtime.email}"
}

resource "google_service_account" "runtime" {
  project      = local.project
  account_id   = "earful-runtime"
  display_name = "Earful app runtime"
}

resource "google_project_iam_member" "runtime_cloudsql" {
  project = local.project
  role    = "roles/cloudsql.client"
  member  = "serviceAccount:${google_service_account.runtime.email}"
}

# Cloud Run pulls images with its service agent, and the registry lives
# in ops — materialize the agent and grant it cross-project read.
resource "google_project_service_identity" "run" {
  provider = google-beta
  project  = local.project
  service  = "run.googleapis.com"
}

resource "google_artifact_registry_repository_iam_member" "run_agent_pulls" {
  project    = local.boot.ops_project_id
  location   = local.region
  repository = local.boot.artifact_registry_repo_id
  role       = "roles/artifactregistry.reader"
  member     = "serviceAccount:${google_project_service_identity.run.email}"
}

module "cloudsql" {
  source = "../../modules/cloudsql"

  project = local.project
  region  = local.region
  tier    = "db-f1-micro"
  # Disposable data: no PITR, no deletion protection (that's pro).
}

# Staging is a test bench, not a public site: the app walls every route
# (probes excepted) behind HTTP Basic Auth and refuses to boot without
# the credential. Generated so one apply provisions it — a shell secret
# with no version would brick revision startup. Read it back with
# `tofu output -raw staging_basic_auth`; the same value feeds the
# STG_BASIC_AUTH GitHub secret so the smoke suite can get through.
resource "random_password" "basic_auth" {
  length  = 24
  special = false
}

module "secrets" {
  source = "../../modules/secrets"

  project           = local.project
  accessor_sa_email = google_service_account.runtime.email

  generated = {
    DATABASE_URL       = module.cloudsql.database_url
    STAGING_BASIC_AUTH = "earful:${random_password.basic_auth.result}"
  }
}

# The org (Workspace-created) enables Domain Restricted Sharing by
# default, which rejects the allUsers invoker binding a public survey app
# needs. This project-level exception restores allow-all — for THIS
# project only; the rest of the org keeps the restriction.
resource "google_org_policy_policy" "allow_public_iam" {
  name   = "projects/${local.project}/policies/iam.allowedPolicyMemberDomains"
  parent = "projects/${local.project}"

  spec {
    rules {
      allow_all = "TRUE"
    }
  }
}

module "app" {
  source = "../../modules/run-service"

  project               = local.project
  region                = local.region
  image                 = local.seed_image
  service_account_email = google_service_account.runtime.email
  sql_connection_name   = module.cloudsql.connection_name

  env = {
    APP_ENV  = "staging"
    BASE_URL = local.base_url
    # console on purpose, forever, on staging: emails land in Cloud
    # Logging, which is exactly how the deploy smoke gate reads magic
    # links back (e2e E2E_LINK_SOURCE=logging). "Staging never sends
    # real email" is also enforced by the app — APP_ENV=staging refuses
    # to boot with any other sender (internal/config).
    EMAIL_SENDER = "console"
    EMAIL_FROM   = "hello@mail.tryearful.com"
    # Same switch as production, so staging can prove an AI change
    # before production sees it (see envs/pro for the reasoning).
    AI_PROVIDER         = var.ai_provider
    TRANSCRIBE_PROVIDER = var.transcribe_provider
    VERTEX_PROJECT      = local.project
    VERTEX_LOCATION     = local.region
    AI_MODEL            = var.ai_model
    AI_MODEL_ANALYZE    = var.ai_model_analyze
    LOG_LEVEL           = "info"
  }

  secret_env = {
    DATABASE_URL       = module.secrets.secret_ids["DATABASE_URL"]
    STAGING_BASIC_AUTH = module.secrets.secret_ids["STAGING_BASIC_AUTH"]
  }

  depends_on = [
    google_artifact_registry_repository_iam_member.run_agent_pulls,
    google_org_policy_policy.allow_public_iam,
  ]
}

module "deploy_sa" {
  source = "../../modules/deploy-sa"

  project          = local.project
  region           = local.region
  runtime_sa_id    = google_service_account.runtime.name
  registry_project = local.boot.ops_project_id
  registry_repo_id = local.boot.artifact_registry_repo_id
  registry_role    = "roles/artifactregistry.writer"
  grant_log_reader = true # smoke gate reads magic links from Cloud Logging
  wif_pool_name    = local.boot.wif_pool_name
  principal_suffix = "attribute.repository/${local.boot.github_repo}"
}

module "domain" {
  source = "../../modules/domain"

  project       = local.project
  region        = local.region
  service_name  = module.app.service_name
  domain        = var.custom_domain
  dns_project   = local.boot.ops_project_id
  dns_zone_name = local.boot.dns_zone_name
}

module "monitoring" {
  source = "../../modules/monitoring"

  project           = local.project
  env_name          = "stg"
  host              = local.host
  sql_instance_name = module.cloudsql.instance_name
  alert_email       = var.alert_email
}

# Staging logs contain magic links (console sender) — they're
# 15-minute-lived credentials for throwaway accounts, but no reason to
# keep them the default 30 days.
resource "google_logging_project_bucket_config" "default" {
  project        = local.project
  location       = "global"
  bucket_id      = "_Default"
  retention_days = 7
}
