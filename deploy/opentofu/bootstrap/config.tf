# Operator values that are neither derivable from the cloud nor safe to
# commit, read from one Secret Manager secret in the production project.
#
# What this buys is worth stating precisely, because the obvious reading
# is wrong. These are not confidential: DNS records are published to the
# world by definition — the SPF policy, the DKIM public key, DMARC and
# the verification codes all answer a dig from anywhere — and tofu owns
# them as resources, so their values sit in state whichever way they
# arrive. Nothing is hidden by this. What it gives is a durable,
# versioned copy of values that otherwise lived in exactly one place:
# one gitignored file on one workstation.
#
# The read is GATED rather than unconditional because this root creates
# the project that holds the secret. On a first apply there is nothing
# to read yet, and a data source is evaluated whether or not anything
# consumes its result — so an ungated read would fail a from-zero
# bootstrap even with every value supplied. Passing the values as
# variables skips the read, which is both the from-zero path and the way
# back in when Secret Manager is unreachable.
#
#   gcloud secrets create bootstrap-config --project earful-pro-<sfx> \
#     --replication-policy automatic
#   printf '%s' "$JSON" | gcloud secrets versions add bootstrap-config \
#     --project earful-pro-<sfx> --data-file=-
#
# The project is named from locals rather than read off
# google_project.pro, the same trade main.tf:23-28 makes for the
# quota_ops alias and for the same kind of reason: depending on the
# resource defers the read to apply, an apply-time value is unknown at
# plan, and the record set below keys its for_each off these records —
# which a plan cannot do with unknown keys. Naming the project keeps the
# read at plan time, where it has to be. The count above is what makes
# that safe before the project exists.

data "google_secret_manager_secret_version_access" "bootstrap_config" {
  count = var.support_email == null || var.mail_dns_records == null ? 1 : 0

  project = local.projects.pro.id
  secret  = var.config_secret_id
}

locals {
  # secret_data is a sensitive attribute, and for_each rejects a
  # sensitive value outright — modules/secrets meets the same wall over
  # its own map and answers it the same way. Note the condition counts
  # instances rather than testing the value: comparing against a
  # sensitive value produces a sensitive result, which would re-mark the
  # whole conditional and land us back here.
  #
  # Decoding over "{}" when the read is gated off, rather than skipping
  # the decode behind another conditional, is also forced: both branches
  # of a conditional must unify to one type, and a decoded object does
  # not unify with an empty one. It has the better failure mode anyway —
  # malformed JSON stays an error, where a try() would swallow it into an
  # empty config and present as the records being deleted.
  config_json = length(data.google_secret_manager_secret_version_access.bootstrap_config) > 0 ? nonsensitive(
    data.google_secret_manager_secret_version_access.bootstrap_config[0].secret_data
  ) : "{}"

  secret_config = jsondecode(local.config_json)

  support_email = coalesce(var.support_email, try(local.secret_config.support_email, null))

  # Both sources are routed through JSON before being picked between,
  # because a conditional's branches must unify to one type and a typed
  # list(object) never unifies with a decoded tuple — coalesce refuses
  # for the same reason. Encoding first makes both branches a string.
  #
  # The loop then normalises to one shape: the variable's type supplies
  # optional(ttl, 300), jsondecode does no such thing, and a record
  # reaching the record set without a ttl fails rather than defaulting.
  records_json = var.mail_dns_records != null ? jsonencode(var.mail_dns_records) : jsonencode(try(local.secret_config.mail_dns_records, []))

  mail_dns_records = [
    for r in jsondecode(local.records_json) : {
      name    = r.name
      type    = r.type
      ttl     = try(r.ttl, 300)
      rrdatas = r.rrdatas
    }
  ]
}
