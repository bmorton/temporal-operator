# Security Policy

## Supported versions

While the project is pre-1.0, only the latest released minor version receives
security fixes. A formal support matrix will be published at the 1.0 release.

## Reporting a vulnerability

**Please do not report security vulnerabilities through public GitHub issues,
discussions, or pull requests.**

Instead, report them privately using GitHub's
[private vulnerability reporting](https://github.com/bmorton/temporal-operator/security/advisories/new)
feature ("Report a vulnerability" under the repository's **Security** tab).

Please include as much of the following as you can:

- The type of issue (e.g. privilege escalation, RBAC misconfiguration, secret
  exposure, injection).
- Affected versions and component (controller, webhook, CRD, Helm chart).
- Step-by-step reproduction instructions and, if possible, a proof of concept.
- The impact of the issue and how an attacker might exploit it.

## Disclosure process

1. We will acknowledge receipt of your report within **3 business days**.
2. We will investigate and provide an initial assessment within **10 business
   days**.
3. We will work with you on a fix and a coordinated disclosure timeline,
   crediting you unless you prefer to remain anonymous.
4. Fixes are released and a security advisory is published once a patch is
   available.
