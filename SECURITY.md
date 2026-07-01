# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| 0.1.x   | :white_check_mark: |
| < 0.1   | :x:                |

## Reporting a Vulnerability

If you discover a security vulnerability in Hermem, please report it responsibly.

**Do NOT open a public GitHub issue for security vulnerabilities.**

Instead, please email security concerns to: [security@hermem.dev](mailto:security@hermem.dev)

### What to include

- Description of the vulnerability
- Steps to reproduce
- Potential impact
- Suggested fix (if any)

### Response Timeline

- **Acknowledgment**: Within 48 hours
- **Initial assessment**: Within 1 week
- **Fix timeline**: Depends on severity

### Security Best Practices

When running Hermem in production:

1. **Enable authentication**: Configure API keys in `hermem.ini`
2. **Use HTTPS**: Always run behind a TLS-terminating proxy
3. **Restrict access**: Limit network access to the API port
4. **Keep updated**: Apply security updates promptly
5. **Audit logs**: Monitor access logs for suspicious activity

## Scope

The following are in scope for security reports:

- Authentication/authorization bypass
- SQL injection
- Remote code execution
- Information disclosure
- Denial of service vulnerabilities

## Out of Scope

- Denial of service via resource exhaustion (rate limiting is the mitigation)
- Issues in dependencies (report to the dependency maintainer)
- Physical security
