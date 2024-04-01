package manifest

type Operator string

const (
	DoesNotExist Operator = "!"
	Equals       Operator = "="
	DoubleEquals Operator = "=="
	In           Operator = "in"
	NotEquals    Operator = "!="
	NotIn        Operator = "notin"
	Exists       Operator = "exists"
	GreaterThan  Operator = "gt"
	LessThan     Operator = "lt"
)

// SelectorRule represents a single math-rule used by [LabelSelector] type to matching [Labels].
// Nil value doesn't match anything.
type Requirement struct {
	// Key is the name of the key to select
	Key string `json:"key,omitempty" yaml:"key,omitempty" `
	// Op is a math operation to perform. See [LabelSelectorOperator] doc for more info.
	Operator Operator `json:"operator,omitempty" yaml:"operator,omitempty" `
	// Values is an optional list of value to apply [SelectorRule.Op] to. For Operator like [Exist] the list must be empty.
	Values []string `json:"values,omitempty" yaml:"values,omitempty" `
}

type Requirements []Requirement

// Selector is an interface for objects that can apply rules to match [Labels]
type Selector interface {
	// Matches returns true if the selector matches given label set.
	Matches(labels Labels) bool

	// Empty returns true if the selector is empty and thus will much everything.
	Empty() bool

	// Requirements returns collection of Requirements to expose more selection information.
	// FIXME: Leaking implementation details
	Requirements() (requirements Requirements, selectable bool)
}

// SearchQuery represent query object accepted by APIs that implement pagination and label based object selection.
type SearchQuery struct {
	Selector Selector

	Offset uint `uri:"offset" form:"offset" json:"offset,omitempty" yaml:"offset,omitempty" xml:"offset"`
	Limit  uint `uri:"limit" form:"limit" json:"limit,omitempty" yaml:"limit,omitempty" xml:"limit"`
}
