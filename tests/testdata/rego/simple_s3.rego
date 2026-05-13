package policy

# Deny public-read S3 buckets — covers the input.attributes path Casper
# passes to every policy.
deny[msg] {
	input.type == "aws_s3_bucket"
	input.attributes.acl == "public-read"
	msg := sprintf("s3 bucket %s is public-read", [input.identifier])
}
