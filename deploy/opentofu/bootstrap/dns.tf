# The tryearful.com zone.

resource "google_dns_managed_zone" "tryearful" {
  project     = google_project.ops.project_id
  name        = "tryearful-com"
  dns_name    = "tryearful.com."
  description = "tryearful.com — GitHub Pages apex, Workspace mail, Earful app/stg"

  depends_on = [google_project_service.apis]
}

# GitHub Pages

resource "google_dns_record_set" "apex_a" {
  project      = google_project.ops.project_id
  managed_zone = google_dns_managed_zone.tryearful.name
  name         = google_dns_managed_zone.tryearful.dns_name
  type         = "A"
  ttl          = 3600
  rrdatas = [
    "185.199.108.153",
    "185.199.109.153",
    "185.199.110.153",
    "185.199.111.153",
  ]
}

resource "google_dns_record_set" "apex_aaaa" {
  project      = google_project.ops.project_id
  managed_zone = google_dns_managed_zone.tryearful.name
  name         = google_dns_managed_zone.tryearful.dns_name
  type         = "AAAA"
  ttl          = 3600
  rrdatas = [
    "2606:50c0:8000::153",
    "2606:50c0:8001::153",
    "2606:50c0:8002::153",
    "2606:50c0:8003::153",
  ]
}

resource "google_dns_record_set" "www" {
  project      = google_project.ops.project_id
  managed_zone = google_dns_managed_zone.tryearful.name
  name         = "www.${google_dns_managed_zone.tryearful.dns_name}"
  type         = "CNAME"
  ttl          = 3600
  rrdatas      = ["tryearful.github.io."]
}

# GitHub only refuses another account's claim on a domain it can see is
# yours, so this record is what keeps tryearful.com from being pointed
# at someone else's Pages site. The challenge is issued with a
# mixed-case owner name; lookups are case-insensitive, so the lowercase
# record answers it, and lowercase is also how Cloud DNS reads a name
# back — spelling it that way here keeps the two in agreement.

resource "google_dns_record_set" "github_pages_challenge" {
  project      = google_project.ops.project_id
  managed_zone = google_dns_managed_zone.tryearful.name
  name         = "_github-pages-challenge-tryearful.${google_dns_managed_zone.tryearful.dns_name}"
  type         = "TXT"
  ttl          = 3600
  rrdatas      = ["\"1f2cec1f56fa1be9bbd5125120e099\""]

  # Losing this record leaves the site serving exactly as before, so
  # nothing would notice: no uptime check covers a TXT lookup, a deleted
  # DNS record is an error nowhere, and the damage is a claim someone
  # else is now free to make. This blocks `tofu destroy` and any change
  # that would replace the record rather than update it. It does NOT
  # survive deletion of this block — a lifecycle rule is part of the
  # resource it guards, so removing the resource removes the guard too.
  # tests/dns_records.tftest.hcl covers that case.
  lifecycle {
    prevent_destroy = true
  }
}

# Google Workspace mail

resource "google_dns_record_set" "apex_mx" {
  project      = google_project.ops.project_id
  managed_zone = google_dns_managed_zone.tryearful.name
  name         = google_dns_managed_zone.tryearful.dns_name
  type         = "MX"
  ttl          = 3600
  rrdatas      = ["1 smtp.google.com."]

  # Losing this record stops mail reaching the operator's mailbox, and
  # nothing would page: there is no uptime check on an MX lookup, and a
  # deleted DNS record is an error nowhere. This blocks `tofu destroy`
  # and any change that would replace the record rather than update it.
  # It does NOT survive deletion of this block — a lifecycle rule is part
  # of the resource it guards, so removing the resource removes the
  # guard too. tests/dns_records.tftest.hcl covers that case.
  lifecycle {
    prevent_destroy = true
  }
}

resource "google_dns_record_set" "apex_txt" {
  project      = google_project.ops.project_id
  managed_zone = google_dns_managed_zone.tryearful.name
  name         = google_dns_managed_zone.tryearful.dns_name
  type         = "TXT"
  ttl          = 3600
  rrdatas = [
    # Original token (pre-dates this configuration; Workspace-era) kept
    # alongside santiago@'s 2026-07-24 Search Console token: multiple
    # verification TXTs coexist fine, and Cloud Run domain mappings need
    # the applying account itself to be a verified owner.
    "\"google-site-verification=7nIancEWTagVFWdVXfzSD1kqB35E7RrtWHqBdafblJo\"",
    "\"google-site-verification=Em37NDvotD_h6Pwnu3T6WpvjU582hbn1Jw79bc7w69I\"",
    # SPF authorizes senders, so it lists only what actually sends. The
    # registrar's include was dropped once the mailbox moved: MX points
    # at Workspace alone, nothing sends through the registrar any more,
    # and its ranges are shared — an include that no longer carries mail
    # still lets every other tenant on those addresses pass SPF for this
    # domain. Brevo is not here either; it signs and sends as the mail
    # subdomain, which carries its own SPF.
    "\"v=spf1 include:_spf.google.com ~all\"",
  ]

  # Losing this record stops mail reaching the operator's mailbox, and
  # nothing would page: there is no uptime check on an MX lookup, and a
  # deleted DNS record is an error nowhere. This blocks `tofu destroy`
  # and any change that would replace the record rather than update it.
  # It does NOT survive deletion of this block — a lifecycle rule is part
  # of the resource it guards, so removing the resource removes the
  # guard too. tests/dns_records.tftest.hcl covers that case.
  lifecycle {
    prevent_destroy = true
  }
}

resource "google_dns_record_set" "google_dkim" {
  project      = google_project.ops.project_id
  managed_zone = google_dns_managed_zone.tryearful.name
  name         = "google._domainkey.${google_dns_managed_zone.tryearful.dns_name}"
  type         = "TXT"
  ttl          = 3600
  rrdatas = [
    join(" ", [
      "\"v=DKIM1;k=rsa;p=MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAviroVaxvQeirVNUu0kywenwA6aWFN1msxWEfaS6+NklxBpo3ttfMTT5q1qV1uSufZ3+/E/Lam5apB+mPgUrC4dXasp\"",
      "\"phks8QnJ38rmSOCOSISOOs1pGV34eB+5cih0IBHBg6c1ZbaDSXsMsPYuEU34H2bIB3kq5rC23Yy+dUkEtXKpuLaHDCEDKv1ON6tkqq3igJyWSNyqX9pWlup9JWgo2N9Rrh2a1kxA9Hf3SVluXMompP\"",
      "\"allsF2AeWaEG5V5UgLMjWELCJE/62DN7dnapzij11UkstFYFJrMrqG8jfo1cOGlyK5arIWmts5/CflBWS5YqxXnUs/ippdNrL3UVowIDAQAB\"",
    ])
  ]

  # Losing this record stops mail reaching the operator's mailbox, and
  # nothing would page: there is no uptime check on an MX lookup, and a
  # deleted DNS record is an error nowhere. This blocks `tofu destroy`
  # and any change that would replace the record rather than update it.
  # It does NOT survive deletion of this block — a lifecycle rule is part
  # of the resource it guards, so removing the resource removes the
  # guard too. tests/dns_records.tftest.hcl covers that case.
  lifecycle {
    prevent_destroy = true
  }
}

# Brevo sending domain (mail.tryearful.com)

resource "google_dns_record_set" "mail" {
  for_each = {
    for r in var.mail_dns_records : "${r.name}-${r.type}" => r
  }

  project      = google_project.ops.project_id
  managed_zone = google_dns_managed_zone.tryearful.name
  name         = "${each.value.name}.${google_dns_managed_zone.tryearful.dns_name}"
  type         = each.value.type
  ttl          = each.value.ttl
  rrdatas      = each.value.rrdatas
}
