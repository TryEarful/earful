# Production (M9-T1): staging's shape plus what durability demands —
# PITR + deletion protection on Cloud SQL, the immutable export pipeline
# (backups module), the webhook secret, and the Brevo flip
# (email_sender variable; console only until the API key secret has a
# version — see deploy/opentofu/README.md step 9).
#
# init:
#   tofu init \
#     -backend-config="bucket=earful-tofu-state-<sfx>" \
#     -backend-config="prefix=pro"

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
  project = local.boot.pro_project_id
  region  = local.boot.region

  run_app_host = "earful-${local.boot.pro_project_number}.${local.region}.run.app"
  host         = var.custom_domain != "" ? var.custom_domain : local.run_app_host
  base_url     = "https://${local.host}"

  seed_image = "${local.boot.artifact_registry}/earful:seed"
}

# See envs/stg: orgpolicy et al. need an explicit quota project.
provider "google" {
  region                = local.boot.region
  billing_project       = local.boot.pro_project_id
  user_project_override = true
}

provider "google-beta" {
  region                = local.boot.region
  billing_project       = local.boot.pro_project_id
  user_project_override = true
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

  project             = local.project
  region              = local.region
  tier                = "db-g1-small"
  pitr_enabled        = true
  deletion_protection = true
}

# Path secret for the ESP bounce webhook (/webhooks/email/{secret}) —
# pre-provisioned so the Brevo flip is a variable change, not a secret
# scramble.
resource "random_password" "email_webhook_secret" {
  length  = 32
  special = false
}

module "secrets" {
  source = "../../modules/secrets"

  project           = local.project
  accessor_sa_email = google_service_account.runtime.email

  generated = {
    DATABASE_URL         = module.cloudsql.database_url
    EMAIL_WEBHOOK_SECRET = random_password.email_webhook_secret.result
  }

  # Values never touch tofu: operator adds versions via gcloud (README).
  shells = [
    "BREVO_API_KEY",
    "GOOGLE_CLIENT_SECRET",
  ]
}

# Project-level exception to the org's Domain Restricted Sharing default —
# the public survey app needs the allUsers invoker (see envs/stg note).
resource "google_org_policy_policy" "allow_public_iam" {
  name   = "projects/${local.project}/policies/iam.allowedPolicyMemberDomains"
  parent = "projects/${local.project}"

  spec {
    rules {
      allow_all = "TRUE"
    }
  }
}

# Calling Vertex needs no key: the service's own identity is the
# credential, which is why ai.Vertex uses Application Default
# Credentials and no secret exists to leak.
resource "google_project_iam_member" "runtime_vertex" {
  project = local.project
  role    = "roles/aiplatform.user"
  member  = "serviceAccount:${google_service_account.runtime.email}"
}

# The identity Cloud Scheduler uses to start the nightly purge. It can
# start that one job and do nothing else.
resource "google_service_account" "purge_scheduler" {
  project      = local.project
  account_id   = "earful-purge-scheduler"
  display_name = "Starts the nightly retention purge (M8-T2)"
}

module "app" {
  source = "../../modules/run-service"

  project               = local.project
  region                = local.region
  image                 = local.seed_image
  service_account_email = google_service_account.runtime.email
  sql_connection_name   = module.cloudsql.connection_name

  # M8-T2: retention runs nightly on production, as the same binary the
  # service runs. Staging leaves it off — its data is disposable, and one
  # fewer scheduled job is one fewer thing to watch.
  enable_purge                    = true
  scheduler_service_account_email = google_service_account.purge_scheduler.email

  env = merge(
    {
      APP_ENV      = "production"
      BASE_URL     = local.base_url
      EMAIL_SENDER = var.email_sender
      EMAIL_FROM   = "hello@mail.tryearful.com"
      # AI is configuration, not code: pointing these at Vertex is all
      # that switches transcription, question generation, insights and
      # translation on. Left at "none" until the models below are
      # confirmed against the live publisher list — an unconfigured
      # capability is an absent feature, never a broken button.
      AI_PROVIDER         = var.ai_provider
      TRANSCRIBE_PROVIDER = var.transcribe_provider
      VERTEX_PROJECT      = local.project
      VERTEX_LOCATION     = local.region
      AI_MODEL            = var.ai_model
      AI_MODEL_ANALYZE    = var.ai_model_analyze
      AI_DAILY_BUDGET_EUR = tostring(var.ai_daily_budget_eur)
      LOG_LEVEL           = "info"
      # M12: production is invite-only — one-shot codes create accounts,
      # email+password signs in, zero emails sent. Staging deliberately
      # stays false (its magic links power the deploy smoke gate).
      BETA_MODE = "true"
    },
    var.google_client_id != "" ? { GOOGLE_CLIENT_ID = var.google_client_id } : {},
  )

  # A secret with no version bricks revision startup, so BREVO_API_KEY
  # and GOOGLE_CLIENT_SECRET mount only once their features switch on —
  # by which point the README has the operator adding the version first.
  secret_env = merge(
    {
      DATABASE_URL         = module.secrets.secret_ids["DATABASE_URL"]
      EMAIL_WEBHOOK_SECRET = module.secrets.secret_ids["EMAIL_WEBHOOK_SECRET"]
    },
    var.email_sender == "brevo" ? { BREVO_API_KEY = module.secrets.secret_ids["BREVO_API_KEY"] } : {},
    var.google_client_id != "" ? { GOOGLE_CLIENT_SECRET = module.secrets.secret_ids["GOOGLE_CLIENT_SECRET"] } : {},
  )

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
  # reader, not writer: pro NEVER builds — it promotes the digest staging
  # smoke-tested.
  registry_role    = "roles/artifactregistry.reader"
  grant_log_reader = false
  wif_pool_name    = local.boot.wif_pool_name
  # Only tag builds may assume this SA (repo already pinned at the pool
  # provider).
  principal_suffix = "attribute.ref_type/tag"
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
  env_name          = "pro"
  host              = local.host
  sql_instance_name = module.cloudsql.instance_name
  alert_email       = var.alert_email
}

module "backups" {
  source = "../../modules/backups"

  backups_project       = local.boot.backups_project_id
  pro_project           = local.project
  region                = local.region
  sql_instance_name     = module.cloudsql.instance_name
  sql_instance_sa_email = module.cloudsql.service_account_email
  lock_retention        = var.lock_retention
}
