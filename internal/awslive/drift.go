package awslive

import "fmt"

// DriftField is one attribute that differs between Terraform state and live AWS state.
type DriftField struct {
	Field          string `json:"field"`
	TerraformValue string `json:"terraform_value"`
	AWSValue       string `json:"aws_value"`
}

// DetectDrift compares tfAttrs (from Terraform state) against awsAttrs (from AWS API).
// Only fields present in tfAttrs are checked — computed-only AWS fields are ignored.
// Fields with an empty Terraform value are skipped (unknown/computed at plan time).
func DetectDrift(tfAttrs map[string]any, awsAttrs map[string]string) []DriftField {
	var drift []DriftField
	for k, tfRaw := range tfAttrs {
		tfVal := fmt.Sprintf("%v", tfRaw)
		if tfVal == "" || tfVal == "<nil>" {
			continue
		}
		awsVal, ok := awsAttrs[k]
		if !ok {
			continue
		}
		if tfVal != awsVal {
			drift = append(drift, DriftField{
				Field:          k,
				TerraformValue: tfVal,
				AWSValue:       awsVal,
			})
		}
	}
	return drift
}
