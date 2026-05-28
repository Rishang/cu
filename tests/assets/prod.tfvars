region        = "us-east-1"
instance_type = "t2.micro"
replica_count = 2
enable_dns    = true

tags = {
  env  = "prod"
  team = "infra"
}
