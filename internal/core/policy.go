package core

// PolicyDefinition defines an access policy evaluated via CEL.
type PolicyDefinition struct {
	ID          string `json:"id" yaml:"id"`
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description" yaml:"description"`
	Expression  string `json:"expression" yaml:"expression"` // CEL expression, e.g. "claims.ai_access == true"
}
