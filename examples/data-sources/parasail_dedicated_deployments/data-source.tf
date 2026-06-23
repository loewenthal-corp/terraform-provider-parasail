data "parasail_dedicated_deployments" "all" {}

output "deployment_names" {
  value = [for d in data.parasail_dedicated_deployments.all.deployments : d.name]
}
