-- name: AddAbuseEvent :exec
INSERT INTO abuse_log (ip, path, kind) VALUES ($1, $2, $3);
