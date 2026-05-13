package policy

# Deny any aws_s3_bucket — used by the e2e test to verify the rego engine
# auto-discovers this file and fires on proposed buckets via simulate_impact.
deny[msg] {
	input.type == "aws_s3_bucket"
	msg := sprintf("s3 buckets disallowed in this repo (%s)", [input.identifier])
}
