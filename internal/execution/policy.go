package execution

type ExecutionPolicy string

const (
	PolicyParallel   ExecutionPolicy = "PARALLEL"
	PolicySequential ExecutionPolicy = "SEQUENTIAL"
)

type PolicySelector struct{}

func NewPolicySelector() *PolicySelector {
	return &PolicySelector{}
}

func (p *PolicySelector) Select(intent *ExecutionIntent) ExecutionPolicy {
	if len(intent.Legs) <= 1 {
		return PolicyParallel
	}
	return PolicySequential
}
