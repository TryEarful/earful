variable "project" {
  type = string
}

variable "generated" {
  description = "secret_id => value, for values tofu itself produces."
  type        = map(string)
  sensitive   = true
  default     = {}
}

variable "shells" {
  description = "secret_ids created empty; operator adds versions via gcloud."
  type        = list(string)
  default     = []
}

variable "accessor_sa_email" {
  description = "Runtime service account granted secretAccessor per-secret."
  type        = string
}
