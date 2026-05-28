variable "region" {
  default = "us-east-1"
}

variable "instance_type" {
  default = "t2.micro"
}

resource "aws_instance" "web" {
  ami           = "ami-0c55b159cbfafe1f0"
  instance_type = "t2.micro"
  count         = 2

  tags = {
    Name = "production"
    Env  = "prod"
  }
}

resource "aws_s3_bucket" "data" {
  bucket = "my-prod-bucket"
}
