# Security

Tossling carries clipboards, files and keys, so security reports are welcome and handled first.

## Reporting a vulnerability

Please do not open a public issue. Use [private vulnerability reporting](https://github.com/tossling/tossling-server/security/advisories/new)
(the «Report a vulnerability» button on the Security tab) and include what you found, how to reproduce it and what an
attacker gains. You should get an answer within a week. Once a fix is released, the advisory is published with credit,
unless you prefer otherwise.

## Scope

- the HTTP layer of this repository: the setup page, the web panel (sign-in, sessions, CSRF, lockout), the
  `/v1/tossy` API, client IP handling and rate limits;
- the access rules it provisions in the embedded ntfy: a device or a publisher token reaching a channel it should not;
- the Docker image and its defaults.

Issues in ntfy itself go to [binwiederhier/ntfy](https://github.com/binwiederhier/ntfy/security); if you are not
sure where a problem belongs, report it here.

Out of scope: a server operator reading metadata the server needs by design (which channels are used and when),
attacks that need an unlocked device the attacker already controls, and denial of service by flooding your own server.

## Supported versions

Only the latest release gets security fixes.
