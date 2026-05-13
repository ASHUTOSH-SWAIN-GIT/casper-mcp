terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

resource "aws_vpc" "main" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = {
    Name = "${var.name_prefix}-vpc"
    env  = "prod"
  }
}

resource "aws_subnet" "private_a" {
  vpc_id            = aws_vpc.main.id
  cidr_block        = "10.0.1.0/24"
  availability_zone = "us-east-1a"

  tags = {
    Name = "${var.name_prefix}-private-a"
  }
}

resource "aws_subnet" "private_b" {
  vpc_id            = aws_vpc.main.id
  cidr_block        = "10.0.2.0/24"
  availability_zone = "us-east-1b"

  tags = {
    Name = "${var.name_prefix}-private-b"
  }
}

resource "aws_security_group" "app" {
  name        = "${var.name_prefix}-app"
  description = "app servers ingress"
  vpc_id      = aws_vpc.main.id
}

resource "aws_db_subnet_group" "postgres" {
  name       = "${var.name_prefix}-postgres"
  subnet_ids = [aws_subnet.private_a.id, aws_subnet.private_b.id]
}

resource "aws_db_instance" "orders_primary" {
  identifier             = "${var.name_prefix}-postgres"
  engine                 = "postgres"
  engine_version         = "15.4"
  instance_class         = var.instance_class
  allocated_storage      = 20
  db_subnet_group_name   = aws_db_subnet_group.postgres.name
  vpc_security_group_ids = [aws_security_group.app.id]
  storage_encrypted      = true
  deletion_protection    = true
  skip_final_snapshot    = false
  publicly_accessible    = false

  tags = {
    Name = "${var.name_prefix}-postgres"
    env  = "prod"
  }
}

resource "aws_db_instance" "orders_replica" {
  identifier             = "${var.name_prefix}-postgres-replica"
  replicate_source_db    = aws_db_instance.orders_primary.identifier
  instance_class         = var.instance_class
  vpc_security_group_ids = [aws_security_group.app.id]
  storage_encrypted      = true
  publicly_accessible    = false
  skip_final_snapshot    = true
  deletion_protection    = false

  tags = {
    Name = "${var.name_prefix}-postgres-replica"
    env  = "prod"
  }
}

resource "aws_lambda_function" "processor" {
  function_name = "${var.name_prefix}-processor"
  role          = "arn:aws:iam::000000000000:role/lambda-noop"
  handler       = "index.handler"
  runtime       = "nodejs20.x"
  filename      = "noop.zip"
}
