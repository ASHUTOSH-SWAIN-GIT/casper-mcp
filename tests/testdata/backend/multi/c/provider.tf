terraform {
  # Region omitted on purpose — should still produce a valid entry.
  backend "s3" {
    bucket = "acme-state"
    key    = "service-c.tfstate"
  }
}
