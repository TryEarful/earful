# The zone records whose loss nothing else in this repository would
# report, asserted to still be configured: the ones mail depends on, and
# the one that holds this domain's claim on GitHub.
#
# prevent_destroy on those resources stops a plan that would destroy or
# replace them, but it cannot survive deletion of the block it lives in:
# a lifecycle rule is part of the resource it guards, so removing the
# resource removes the guard in the same edit. That is the failure this
# file exists to catch, and it is a plausible one — these records serve
# something outside the application, so nothing in the product breaks
# when they go, no uptime check covers an MX or a TXT lookup, and a
# deleted DNS record is an error nowhere. Mail simply stops arriving,
# and the domain simply becomes claimable again.
#
# Run from bootstrap:  tofu init -backend=false && tofu test
# Mock providers — asserts OUR configuration, no cloud calls.

# Generated mock values are random strings, which some resources here
# reject on format — a service account id has to look like one. Only the
# attributes other resources read back need pinning.
# The quota_ops alias (main.tf) is a SEPARATE provider configuration, and
# mock_provider only covers the one it names. Left unmocked it falls
# through to real credentials, so this file passed on a workstation with
# ADC and failed everywhere else — including CI, which has none. Mocking
# the alias is what makes the run credential-free, which is the only
# state in which it is a net rather than a workstation habit.
mock_provider "google" {
  alias = "quota_ops"
}

mock_provider "google" {
  mock_resource "google_service_account" {
    defaults = {
      name  = "projects/earful-ops-test/serviceAccounts/builder@earful-ops-test.iam.gserviceaccount.com"
      email = "builder@earful-ops-test.iam.gserviceaccount.com"
    }
  }
}

variables {
  billing_account = "000000-000000-000000"
  suffix          = "test"
  org_id          = "000000000000"
  support_email   = "alerts@example.test"

  mail_dns_records = [
    {
      name    = "mail"
      type    = "TXT"
      rrdatas = ["\"v=spf1 include:spf.example.test ~all\""]
    },
  ]
}

run "mail_records_are_still_configured" {
  command = plan

  # Each of the three is load-bearing on its own: MX routes the mail,
  # SPF and the verification tokens live in the apex TXT, and DKIM signs
  # what Workspace sends. Losing any one degrades or stops delivery.
  assert {
    condition     = google_dns_record_set.apex_mx.type == "MX"
    error_message = "the apex MX record is gone — mail to the operator's domain stops being routed, and nothing else in this repository would notice"
  }

  assert {
    condition     = anytrue([for r in google_dns_record_set.apex_txt.rrdatas : startswith(r, "\"v=spf1")])
    error_message = "the apex SPF record is gone — outgoing mail loses its authorized-sender list and starts failing SPF at recipients"
  }

  assert {
    condition     = anytrue([for r in google_dns_record_set.apex_txt.rrdatas : startswith(r, "\"google-site-verification=")])
    error_message = "domain verification TXT is gone — Cloud Run domain mappings require the applying account to be a verified owner, so this breaks the next mapping rather than anything visible today"
  }

  assert {
    condition     = startswith(google_dns_record_set.google_dkim.rrdatas[0], "\"v=DKIM1")
    error_message = "the Workspace DKIM record is gone — mail still sends, but unsigned, which is the quiet half of a deliverability failure"
  }

  # prevent_destroy itself is deliberately not asserted here: lifecycle
  # is a meta-argument rather than an exported attribute, so no test can
  # read it back. That is tolerable, because the two failures divide
  # cleanly — a plan that would destroy a guarded record fails on the
  # guard, and a plan that deleted the block along with its guard fails
  # on the assertions above, which is the case with no other net under
  # it.
}

# Verification is the whole of what stops another GitHub account
# claiming this domain for its own Pages site, and it is asserted here
# rather than left to the guard because the site keeps serving either
# way — there is no symptom to notice.
run "github_pages_verification_is_still_configured" {
  command = plan

  assert {
    condition     = startswith(google_dns_record_set.github_pages_challenge.name, "_github-pages-challenge-")
    error_message = "the GitHub Pages domain-verification record is gone — the site keeps serving, but the domain becomes claimable by another GitHub account again"
  }
}

# The other runs pass the records in, which is the from-zero path and
# leaves the secret unread. This one is the steady-state path: both
# variables null, so config.tf reads the secret and has to decode it
# into the same shape. Overriding the data source keeps the run
# credential-free — nothing here reaches Secret Manager.
#
# A CNAME rather than the TXT the other runs use, because override_data
# takes a literal with no functions available, and a TXT rrdata carries
# embedded quotes that a heredoc would eat.
run "records_can_come_from_the_config_secret" {
  command = plan

  variables {
    support_email    = null
    mail_dns_records = null
  }

  override_data {
    target = data.google_secret_manager_secret_version_access.bootstrap_config
    values = {
      secret_data = "{\"support_email\":\"alerts@example.test\",\"mail_dns_records\":[{\"name\":\"brevo1._domainkey.mail\",\"type\":\"CNAME\",\"rrdatas\":[\"b1.dkim.example.test.\"]}]}"
    }
  }

  # ttl is deliberately absent from that JSON: the variable's type
  # defaults it, jsondecode does not, and a record reaching the record
  # set without one fails the apply rather than falling back. This
  # asserts the two paths normalise to the same shape.
  assert {
    condition     = google_dns_record_set.mail["brevo1._domainkey.mail-CNAME"].ttl == 300
    error_message = "a record supplied without a ttl did not pick up the default — the secret and variable paths are not producing the same shape"
  }

  assert {
    condition     = google_monitoring_notification_channel.budget_email[0].labels.email_address == "alerts@example.test"
    error_message = "support_email did not come back from the secret — budget alerts would have no destination"
  }
}

# The zone is the single point through which certificate issuance, mail
# delivery and the marketing site all pass, so it must not be deletable
# by an apply that happens to remove its records first.
run "zone_refuses_casual_deletion" {
  command = plan

  assert {
    # Unset reads back as null rather than false, and null is the state
    # this wants: absent and explicitly-false both mean the zone refuses
    # to be deleted while it still holds records.
    condition     = google_dns_managed_zone.tryearful.force_destroy != true
    error_message = "force_destroy on the zone would let a destroy take the domain's records with it"
  }
}
