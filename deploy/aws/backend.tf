terraform {
  backend "s3" {
    bucket = "uc-terraform"
    key = "micysql/state/terraform.tfstate"
    encrypt = true
    region = "ap-southeast-2"
    dynamodb_table = "terraform-state-lock-table"
  }
}
