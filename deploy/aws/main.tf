resource "random_id" "deploy_id" {
  byte_length = 8
}

module "micysql" {
  source            = "./modules/lambda"

  function_name     = "micysql-${random_id.deploy_id.hex}"
  runtime           = "provided.al2023"
  handler           = "bootstrap"
  memory_size       = 4096
  timeout           = 300
  architectures     = ["arm64"]

  local_zip_path    = "../../dist/micysql.zip"  # Local path to ZIP file

  s3_bucket         = var.S3_BUCKET
  s3_key            = "micysql/micysql.zip"
  source_code_hash  = filebase64sha256("../../dist/micysql.zip")

  environment_variables = {
    AWS_LAMBDA_EXEC_WRAPPER = "/opt/bootstrap"
    DB_USER = var.DB_USER
    DB_PASS = var.DB_PASS
  }

  layer_arns = [
    "arn:aws:lambda:${var.AWS_REGION}:753240598075:layer:LambdaAdapterLayerArm64:24"
  ]

  enable_function_url = true
}

# This bridge is for client applications (ie WordPress) that uses
# traditional SQL socket (MySQL Wire Protocol) servers. As Lambda doesn't
# allow TCP connections directly, this bridge translates the wire protocol to/from HTTP(s).
module "lambda_layer" {
  source = "./modules/lambda_layer"
  layer_name          = "micysql-bridge"
  description         = "MicySQL Bridge provides a unix socket which translates SQL to a MicySQL lambda function URL"
  license_info        = "Apache-2.0"
  compatible_runtimes = ["provided.al2023", "provided.al2"]
  s3_bucket      = var.S3_BUCKET
  s3_key         = "layers/micysql-bridge.zip"
  local_zip_path = "../../dist/micysql-bridge.zip"
}
