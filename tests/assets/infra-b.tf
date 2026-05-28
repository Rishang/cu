variable "region" {
  default = "eu-west-1"
}

variable "instance_type" {
  default = "t3.small"
}

resource "aws_instance" "web" {
  ami           = "ami-0c55b159cbfafe1f0"
  instance_type = "t3.small"
  count         = 1

  tags = {
    Name = "staging"
    Env  = "stage"
  }
}

resource "aws_s3_bucket" "data" {
  bucket        = "my-stage-bucket"
  force_destroy = true
}
