#!/bin/bash

# Get all open PRs with the dependencies label and created by dependabot
# Include status check information in the JSON output
prs=$(gh pr list --label dependencies --author app/dependabot --json number,statusCheckRollup --jq '.[]')

echo "Found PRs to process..."

while IFS= read -r pr_json; do
    pr_number=$(echo "$pr_json" | jq -r '.number')
    checks_passed=true
    
    echo "Processing PR #$pr_number"
    
    # Check all status checks
    while IFS= read -r check_status; do
        if [ "$check_status" != "SUCCESS" ] && [ "$check_status" != "NEUTRAL" ]; then
            checks_passed=false
            echo "Check failed with status: $check_status"
            break
        fi
    done < <(echo "$pr_json" | jq -r '.statusCheckRollup[].state')
    
    if [ "$checks_passed" = true ]; then
        echo "All checks passed for PR #$pr_number, proceeding with approval..."
        
        # Leave a comment instructing dependabot to squash and merge
        gh pr review "$pr_number" --comment -b "@dependabot squash and merge"
        
        # Approve the PR
        gh pr review "$pr_number" --approve
        
        echo "Approved and commented on PR #$pr_number"
    else
        echo "Skipping PR #$pr_number due to failed checks"
    fi
done < <(echo "$prs")

echo "Done processing all dependency PRs!" 