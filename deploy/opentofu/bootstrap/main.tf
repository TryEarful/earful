# Bootstrap: everything that exists once, before any environment can —
# the four projects, the tofu state bucket, the GitHub Workload Identity
# pool, the DNS zone, the shared Artifact Registry, and the billing
# budgets. Runs on LOCAL state for its first apply; afterwards uncomment
# backend.tf and `tofu init -migrate-state` to move its state into the
# bucket it just created (deploy/opentofu/README.md walks through it).

terraform {
  required_version = ">= 1.6"

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 6.0"
    }
  }
}

provider "google" {
  region = var.region
}

# Billing budgets and Cloud Monitoring reject plain user credentials
# without an explicit quota project. Only the budget/channel resources use
# this alias — everything else (including creating the ops project this
# names) uses the default provider, or we'd have a cycle. The ID is
# spelled out rather than referencing google_project.ops for the same
# reason.
provider "google" {
  alias                 = "quota_ops"
  region                = var.region
  billing_project       = "earful-ops-${var.suffix}"
  user_project_override = true
}

locals {
  projects = {
    ops = {
      id   = "earful-ops-${var.suffix}"
      name = "Earful ops"
      # Shared infrastructure: state, DNS, WIF, registry, budget channel.
      apis = [
        "artifactregistry.googleapis.com",
        "billingbudgets.googleapis.com",
        "cloudbilling.googleapis.com",
        "cloudresourcemanager.googleapis.com",
        "dns.googleapis.com",
        "iam.googleapis.com",
        "iamcredentials.googleapis.com",
        "monitoring.googleapis.com",
        "serviceusage.googleapis.com",
        "storage.googleapis.com",
        "sts.googleapis.com",
      ]
    }
    stg = {
      # Staging carries its own suffix so it can be rebuilt without
      # touching the other three. A project can be lost for reasons that
      # have nothing to do with its contents — ours was suspended by
      # Google on 2026-07-25 — and staging is the one that is meant to be
      # disposable. Set stg_suffix to a fresh value, apply, and rewire;
      # everything else keeps its id. See the runbook.
      id   = "earful-stg-${coalesce(var.stg_suffix, var.suffix)}"
      name = "Earful staging"
      apis = local.env_apis
    }
    pro = {
      id   = "earful-pro-${var.suffix}"
      name = "Earful production"
      # Pro additionally runs the daily export pipeline (M9-T6);
      # Cloud Scheduler is in env_apis because the retention purge
      # (M8-T2) uses it too.
      apis = concat(local.env_apis, [
        "workflows.googleapis.com",
      ])
    }
    backups = {
      id   = "earful-backups-${var.suffix}"
      name = "Earful backups"
      # Deliberately minimal: a bucket and nothing else, so no credential
      # useful elsewhere is useful here (ADR-0008).
      apis = [
        "iam.googleapis.com",
        "storage.googleapis.com",
      ]
    }
  }

  env_apis = [
    # cloudresourcemanager + serviceusage: with the provider's
    # user_project_override, project-level IAM/API reads bill the env
    # project itself and need these enabled there.
    "cloudresourcemanager.googleapis.com",
    "iam.googleapis.com",
    "iamcredentials.googleapis.com",
    "logging.googleapis.com",
    "monitoring.googleapis.com",
    "serviceusage.googleapis.com",
    # For the per-project Domain Restricted Sharing exception the public
    # invoker binding requires (org-created projects inherit the org
    # default that forbids allUsers).
    "orgpolicy.googleapis.com",
    "run.googleapis.com",
    "secretmanager.googleapis.com",
    "sqladmin.googleapis.com",
    "sts.googleapis.com",
    # M6-T1/M5: Vertex AI. Enabled in both environments so the AI
    # features can be switched on by configuration rather than by an
    # apply; nothing is billed until AI_PROVIDER points at it.
    "aiplatform.googleapis.com",
    # M8-T2: the nightly retention purge runs as a Cloud Scheduler job.
    "cloudscheduler.googleapis.com",
  ]

  api_pairs = merge([
    for key, p in local.projects : {
      for api in p.apis : "${key}/${api}" => { project_key = key, api = api }
    }
  ]...)
}

resource "google_project" "ops" {
  project_id      = local.projects.ops.id
  name            = local.projects.ops.name
  billing_account = var.billing_account
  org_id          = var.org_id
  folder_id       = var.folder_id
  deletion_policy = "PREVENT"
}

resource "google_project" "stg" {
  project_id      = local.projects.stg.id
  name            = local.projects.stg.name
  billing_account = var.billing_account
  org_id          = var.org_id
  folder_id       = var.folder_id
  deletion_policy = "PREVENT"
}

resource "google_project" "pro" {
  project_id      = local.projects.pro.id
  name            = local.projects.pro.name
  billing_account = var.billing_account
  org_id          = var.org_id
  folder_id       = var.folder_id
  deletion_policy = "PREVENT"
}

resource "google_project" "backups" {
  project_id      = local.projects.backups.id
  name            = local.projects.backups.name
  billing_account = var.billing_account
  org_id          = var.org_id
  folder_id       = var.folder_id
  deletion_policy = "PREVENT"
}

locals {
  project_resources = {
    ops     = google_project.ops
    stg     = google_project.stg
    pro     = google_project.pro
    backups = google_project.backups
  }
}

resource "google_project_service" "apis" {
  for_each = local.api_pairs

  project            = local.project_resources[each.value.project_key].project_id
  service            = each.value.api
  disable_on_destroy = false
}
