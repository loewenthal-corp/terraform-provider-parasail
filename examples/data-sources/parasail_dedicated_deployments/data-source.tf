data "parasail_dedicated_deployments" "all" {}

# Deployment IDs by name, for example to find the ID to import.
output "deployment_ids" {
  value = { for d in data.parasail_dedicated_deployments.all.deployments : d.name => d.id }
}

output "online_endpoints" {
  value = {
    for d in data.parasail_dedicated_deployments.all.deployments :
    d.name => { base_url = d.base_url, model = d.model_alias }
    if d.status == "ONLINE"
  }
}
