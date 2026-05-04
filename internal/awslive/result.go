package awslive

// LiveStateResult is the output of a describe_live_state call.
type LiveStateResult struct {
	ScopeResources []string        `json:"scope_resources"`
	Resources      []ResourceState `json:"resources"`
	NotInTerraform []UnmanagedItem `json:"not_in_terraform,omitempty"`
	Errors         []ResourceError `json:"errors,omitempty"`
}

// ResourceState holds the Terraform-managed and live AWS state for one resource,
// plus any drift between them.
type ResourceState struct {
	Identifier     string            `json:"identifier"`
	Type           string            `json:"type"`
	TerraformState map[string]any    `json:"terraform_state,omitempty"`
	LiveAWSState   map[string]string `json:"live_aws_state,omitempty"`
	Drift          []DriftField      `json:"drift,omitempty"`
}

// UnmanagedItem is a resource AWS returned that Terraform doesn't track.
type UnmanagedItem struct {
	Type   string `json:"type"`
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// ResourceError captures a per-resource failure without failing the whole call.
type ResourceError struct {
	Resource string `json:"resource"`
	Error    string `json:"error"`
}
