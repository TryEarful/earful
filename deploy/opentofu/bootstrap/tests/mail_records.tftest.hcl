# The records that keep mail reaching the operator's mailbox, asserted
# to still be configured.
#
# prevent_destroy on those resources stops a plan that would destroy or
# replace them, but it cannot survive deletion of the block it lives in:
# a lifecycle rule is part of the resource it guards, so removing the
# resource removes the guard in the same edit. That is the failure this
# file exists to catch, and it is a plausible one — the records belong to
# a mailbox rather than to the application, so nothing in the product
# breaks when they go, no uptime check covers an MX lookup, and a deleted
# DNS record is an error nowhere. Mail simply stops arriving.
#
# Run from bootstrap:  tofu init -backend=false && tofu test
# Mock providers — asserts OUR configuration, no cloud calls.

# Generated mock values are random strings, which some resources here
# reject on format — a service account id has to look like one. Only the
# attributes other resources read back need pinning.
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
