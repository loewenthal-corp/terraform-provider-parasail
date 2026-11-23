# Import a dedicated deployment by its numeric Parasail deployment ID. The
# parasail_dedicated_deployments data source lists the IDs of existing
# deployments. model_access_key, draft_model_access_key, and wait_for_online
# are not returned by Parasail; set them in configuration after importing.
terraform import parasail_dedicated_deployment.example 12345
