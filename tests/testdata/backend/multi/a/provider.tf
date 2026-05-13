terraform {
  backend "s3" {
    bucket = "acme-state"
    key    = "service-a.tfstate"
    region = "us-east-1"
  }
}
