# Anonymity is strong and immutable

Status: accepted — amended by ADR-0009 (unlinked audience aggregates; 2026-07-19)

An anonymous survey stores no email, IP, user-agent, or any identifying data with responses — ever. IPs appear only in a separate short-retention abuse log that is never joinable to responses. Whether a survey is anonymous or invited is fixed at creation and cannot change in any later version.

## Consequences

- Debugging/abuse forensics on anonymous responses is deliberately impossible beyond the unlinked abuse log. Do not "fix" this by adding an IP column.
- Rate limiting and bot defence must work without persisting identifiers on the response path.
- The respondent-facing UI can honestly display the anonymity guarantee; it is load-bearing for the brand.
