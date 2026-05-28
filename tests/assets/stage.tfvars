region        = "eu-west-1"
instance_type = "t3.small"
replica_count = 1
enable_dns    = false

tags = {
  env  = "stage"
  team = "infra"
}
