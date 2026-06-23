config {
  call_module_type = "local"
}

# examples/resources/** and examples/data-sources/** are bare snippets consumed by
# tfplugindocs for the registry "Example Usage" sections, not standalone configs,
# so they intentionally omit terraform{} / required_providers blocks. The runnable
# examples/provider/provider.tf demonstrates the full required_providers block.
rule "terraform_required_version" {
  enabled = false
}

rule "terraform_required_providers" {
  enabled = false
}
