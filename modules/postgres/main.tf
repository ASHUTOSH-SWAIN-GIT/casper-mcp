terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

variable "name" {
  type        = string
  description = "Database name."
}

variable "instance_class" {
  type        = string
  description = "RDS instance class."
}

variable "vpc_id" {
  type        = string
  description = "VPC ID."
}

variable "subnet_ids" {
  type        = list(string)
  description = "Subnets for the database subnet group."
}

resource "aws_db_subnet_group" "this" {
  name       = var.name
  subnet_ids = var.subnet_ids
}

resource "aws_db_instance" "this" {
  identifier           = var.name
  instance_class       = var.instance_class
  engine               = "postgres"
  db_subnet_group_name = aws_db_subnet_group.this.name
}

output "id" {
  value = aws_db_instance.this.id
}
