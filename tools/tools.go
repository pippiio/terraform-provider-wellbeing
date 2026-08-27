package tools

// Format Terraform code for use in documentation.
// If you do not have Terraform installed, you can remove the formatting command, but it is suggested
// to ensure the documentation is formatted properly.
//go:generate terraform fmt -recursive ../examples/

// Generate documentation.
// tfplugindocs is tracked as a tool dependency in go.mod, so this uses the
// pinned version rather than whatever happens to be installed.
//go:generate go tool tfplugindocs generate --provider-dir .. -provider-name wellbeing
