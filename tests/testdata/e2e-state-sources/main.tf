terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }

  backend "s3" {
    bucket = "casper-e2e-bucket-does-not-exist"
    key    = "test.tfstate"
    region = "us-east-1"
  }
}

resource "aws_vpc" "main" {
  cidr_block = "10.0.0.0/16"
}
