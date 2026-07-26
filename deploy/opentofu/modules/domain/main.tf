# Custom domain for a Cloud Run service: the domain mapping (managed
# certificate included) plus its CNAME in the ops-project zone. Gated on
# var.domain being set — env stacks run domainless until the user's
# nameserver cutover, then flip the custom_domain variable.
#
# Prerequisite: the applying identity must be a verified owner of the
# domain (the zone's google-site-verification TXT record pre-dates this
# configuration; see bootstrap/dns.tf — verification already holds).

terraform {
  required_providers {
    google = {
      source = "hashicorp/google"
    }
  }
}

resource "google_cloud_run_domain_mapping" "app" {
  count = var.domain == "" ? 0 : 1

  project  = var.project
  location = var.region
  name     = var.domain

  metadata {
    namespace = var.project
  }

  spec {
    route_name = var.service_name
  }
}

resource "google_dns_record_set" "cname" {
  count = var.domain == "" ? 0 : 1

  project      = var.dns_project
  managed_zone = var.dns_zone_name
  name         = "${var.domain}."
  type         = "CNAME"
  ttl          = 300
  rrdatas      = ["ghs.googlehosted.com."]
}
