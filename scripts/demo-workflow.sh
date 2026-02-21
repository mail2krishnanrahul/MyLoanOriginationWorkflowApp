#!/bin/bash
set -e

echo "🎬 Simulating Home Loan Origination Workflow Demo!"
echo "==================================================="

# Call the integration script which essentially does the flow
./scripts/integration-test.sh

echo ""
echo "Dashboard Analytics Test:"
curl -s -X GET "http://localhost:8080/api/v1/cases" | grep -o '"id":"[^"]*' | cut -d'"' -f4 | xargs -I {} echo "Found Case: {}"

echo "Demo complete."
