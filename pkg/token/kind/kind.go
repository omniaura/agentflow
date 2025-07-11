package kind

//go:generate go tool stringer -type=Kind

type Kind int

// TODO: add KindDoc, KindVarDoc
// TODO: add .var var predeclare optional; sets the types
// example:
//
// .title say hello to your new friends
// .var names string list join="\n"
// Please say hello to:
// <!names>
//
// INPUT:
// Joe,Mary,Jane
//
// OUTPUT:
// Please say hello to:
// Joe
// Mary
// Jane

const (
	Unset Kind = iota

	// Basic bracket structure
	OpenBracket  // "<"
	CloseBracket // ">"

	// Directive types (what comes after <)
	DirectiveVar  // "!" in "<!username>"
	DirectiveCond // "?" in "<?condition>"
	DirectiveEnd  // "/" in "</tag>"
	DirectiveElse // "else" in "<else>"

	// Title directive
	TitleDirective // ".title"
	TitleText      // "System Prompt" in ".title System Prompt"

	// Content types (used in variables, conditionals, etc.)
	VarName     // "username", "user.premium", etc.
	TypeName    // "int", "bool", "string", "float32", "float64"
	Operator    // "eq", "gte", "lte", "gt", "lt", "ne"
	StringValue // "gold" in "<?tier eq "gold">"
	IntValue    // "5" in "<?count gte 5>"
	BoolValue   // "true" in "<?active eq true>"

	// Content
	Text       // Regular text content
	Whitespace // Spaces, tabs, newlines (separators)

	// Future extension tokens (for later)
	// RawBlock       // For future raw block support
	// Comment        // For future comment support
)

func (k Kind) IsTag() bool {
	switch k {
	case
		OpenBracket, CloseBracket,
		DirectiveVar, DirectiveCond, DirectiveEnd, DirectiveElse,
		VarName, TypeName, Operator, StringValue, IntValue, BoolValue:
		return true
	}
	return false
}

func (k Kind) IsBracket() bool {
	switch k {
	case OpenBracket, CloseBracket:
		return true
	}
	return false
}

func (k Kind) IsDirective() bool {
	switch k {
	case DirectiveVar, DirectiveCond, DirectiveEnd, DirectiveElse:
		return true
	}
	return false
}

func (k Kind) IsValue() bool {
	switch k {
	case VarName, TypeName, StringValue, IntValue, BoolValue:
		return true
	}
	return false
}

func (k Kind) IsContent() bool {
	switch k {
	case Text:
		return true
	}
	return false
}

func (k Kind) IsStructural() bool {
	switch k {
	case Whitespace, TitleDirective:
		return true
	}
	return false
}
