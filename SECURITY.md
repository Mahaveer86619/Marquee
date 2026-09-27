# Security policy

## Supported versions

Marquee is in early development and has no stable release yet. Security fixes are applied to the `main` branch.

## Reporting a vulnerability

Please report vulnerabilities privately through GitHub's "Report a vulnerability" feature on the repository's Security tab. Do not open a public issue.

Include a description of the issue, the affected component, and steps to reproduce it if possible. You can expect an acknowledgement within seven days.

## Scope

The following areas are security-sensitive:

- The core API and stream endpoints, which must listen only on the loopback interface
- Marquee Link: pairing, key handling, feed replication and co-watch sessions
- Storage of secrets such as API keys and backup credentials
- Handling of untrusted input: torrent metadata, subtitle files, media containers and peer payloads
