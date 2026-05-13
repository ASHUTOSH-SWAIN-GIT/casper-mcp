terraform {
  backend "s3" {
    bucket = "acme-state"
    key    = "service-b.tfstate"
    region = "eu-west-1"
  }
}
