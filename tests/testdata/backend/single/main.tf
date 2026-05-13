terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }

  backend "s3" {
    bucket         = "tofu-backend-429032495558"
    key            = "base-new.tfstate"
    region         = "ap-south-1"
    dynamodb_table = "tofu-backend"
    encrypt        = true
  }
}
