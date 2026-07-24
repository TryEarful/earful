output "bucket_name" {
  value = google_storage_bucket.exports.name
}

output "workflow_name" {
  value = google_workflows_workflow.export.name
}
