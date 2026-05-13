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

resource "aws_db_instance" "this" {
  identifier     = var.name
  instance_class = var.instance_class
  engine         = "postgres"
}

output "id" {
  value = aws_db_instance.this.id
}
