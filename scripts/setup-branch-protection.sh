#!/bin/bash
# Setup branch protection rules for develop and main branches.
# Requires: gh CLI with appropriate permissions (admin or push access).
#
# Usage:
#   ./scripts/setup-branch-protection.sh
#
# This script requires the CI workflow status check context "ci" to pass
# before merging into develop or main branches.

set -euo pipefail

REPO="${REPO:-ytnobody/MADFLOW}"

echo "Setting up branch protection for ${REPO}..."

for BRANCH in develop main; do
  echo "  Configuring branch: ${BRANCH}"
  gh api -X PUT "repos/${REPO}/branches/${BRANCH}/protection" \
    --input - << 'EOF'
{
  "required_status_checks": {
    "strict": true,
    "contexts": ["ci"]
  },
  "enforce_admins": false,
  "required_pull_request_reviews": null,
  "restrictions": null
}
EOF
  echo "  Done: ${BRANCH}"
done

echo "Branch protection setup complete."
