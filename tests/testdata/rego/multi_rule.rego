package policy

# Two rules in one file — both should fire independently.

deny[msg] {
	input.type == "aws_lambda_function"
	not input.tags.owner
	msg := "lambda function missing owner tag"
}

deny[msg] {
	input.type == "aws_lambda_function"
	input.attributes.runtime == "nodejs12.x"
	msg := "lambda runtime nodejs12.x is end-of-life"
}
