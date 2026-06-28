package doktriai.authz

# Defaults
default allow = false
default requires_approval = false
default reason = ""

# Helper rules: Check if image has an approved prefix
image_approved {
    approved_prefixes := [
        "nginx",
        "redis",
        "node",
        "mysql",
        "postgres",
        "doktri/",
        "doktriai/"
    ]
    prefix := approved_prefixes[_]
    startswith(input.spec.image, prefix)
}

# Helper rules: Check if env variable key is sensitive
is_sensitive_key(key) {
    sensitive_patterns := [
        "SECRET", "KEY", "TOKEN", "PASSWORD", "PASSWD", "CREDENTIAL", "PRIVATE"
    ]
    pattern := sensitive_patterns[_]
    contains(upper(key), pattern)
}

# Helper rules: Check if env map has any sensitive key
has_sensitive_env {
    is_sensitive_key(object.keys(input.spec.env)[_])
}

# Main authorization rules
allow {
    input.role == "admin"
    image_approved
    input.spec.replicas <= 50
}

allow {
    input.role == "operator"
    image_approved
    input.spec.replicas <= 50
    input.action == "read"
}

allow {
    input.role == "operator"
    image_approved
    input.spec.replicas <= 50
    input.action == "apply"
}

# Rejects if replica count exceeds absolute max limit
reason = "replica count exceeds maximum absolute limit of 50" {
    input.spec.replicas > 50
}

# Rejects if image prefix is unapproved
reason = "image registry prefix is not in allowlist" {
    not image_approved
}

# Requires human approval (PTE gate) rules:
# 1. If replicas > safe auto-apply threshold (5)
requires_approval {
    input.spec.replicas > 5
}
reason = sprintf("replica count %d exceeds safe auto-apply threshold of 5", [input.spec.replicas]) {
    input.spec.replicas > 5
}

# 2. If env variables contain sensitive patterns
requires_approval {
    has_sensitive_env
}
reason = "env variables contain sensitive credential key patterns" {
    has_sensitive_env
}

# 3. All deletion commands require PTE approval
requires_approval {
    input.action == "delete"
}
reason = sprintf("deletion of workload %q requires human approval", [input.spec.name]) {
    input.action == "delete"
}
