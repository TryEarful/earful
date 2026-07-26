# The tryearful.com zone. CRITICAL: this domain already serves the
# marketing site (GitHub Pages, apex + www) and the operator's mailbox
# (Google Workspace MX + DKIM). Every live record was captured with dig
# from the registrar's zone and is replicated below byte-for-byte, so the
# nameserver change at the registrar (Gandi) is invisible to both. Do not prune anything here
# without re-running the pre-cutover dig-diff gate in the README.
#
# Deliberate change vs the old zone: app.tryearful.com's A record
# (8.228.238.150 — recorded here for rollback) is NOT replicated; the pro
# env stack points app at Cloud Run instead, per the user's instruction.

resource "google_dns_managed_zone" "tryearful" {
  project     = google_project.ops.project_id
  name        = "tryearful-com"
  dns_name    = "tryearful.com."
  description = "tryearful.com — GitHub Pages apex, Workspace mail, Earful app/stg"

  depends_on = [google_project_service.apis]
}

# --- GitHub Pages (marketing site) ---

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

# --- Google Workspace mail (tryearful.com primary mailbox) ---

resource "google_dns_record_set" "apex_mx" {
  project      = google_project.ops.project_id
  managed_zone = google_dns_managed_zone.tryearful.name
  name         = google_dns_managed_zone.tryearful.dns_name
  type         = "MX"
  ttl          = 3600
  rrdatas      = ["1 smtp.google.com."]
}

# One INTENTIONAL improvement over the old Gandi zone (user-approved
# 2026-07-24; everything else in this file is a byte-for-byte replica):
# the old SPF omitted _spf.google.com despite all mail flowing through
# Workspace, and ended in the do-nothing "?all". Fixed here: Google's
# senders authorized, Gandi's include kept until confirmed unused
# (includes only authorize — keeping it cannot break anything), softfail.
# NOTE for the pre-cutover dig-diff gate: apex TXT is EXPECTED to differ
# from Gandi's live answer unless the same fix is mirrored there.
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
    "\"v=spf1 include:_spf.google.com include:_mailcust.gandi.net ~all\"",
  ]
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
}

# --- Brevo sending domain (mail.tryearful.com), filled at the Brevo step ---

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
