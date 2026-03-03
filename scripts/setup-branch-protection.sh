#!/bin/bash
# Setup branch protection rules and repository settings for MADFLOW.
# Requires: gh CLI with appropriate permissions (admin or push access).
#
# Usage:
#   ./scripts/setup-branch-protection.sh
#
# This script:
#   1. Requires the CI workflow status check context "ci" to pass
#      before merging into develop or main branches.
#   2. Enables automatic deletion of head branches after PR merge.

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
  "enforce_admins": true,
  "required_pull_request_reviews": null,
  "restrictions": null
}
EOF
  echo "  Done: ${BRANCH}"
done

echo "Enabling auto-delete of head branches after merge..."
gh api -X PATCH "repos/${REPO}" -f delete_branch_on_merge=true --silent
echo "Done: delete_branch_on_merge enabled."

echo "Repository setup complete."
