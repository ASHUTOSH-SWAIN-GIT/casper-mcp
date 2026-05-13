package policy

# RDS without deletion_protection = "true" should be blocked.
deny[msg] {
	input.type == "aws_db_instance"
	input.attributes.deletion_protection != "true"
	msg := sprintf("rds %s missing deletion_protection", [input.identifier])
}
