package kind

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
	EndTag
	OptionalBlock
	Title
	Text
	// TODO: add var parameters
	// such as:
	// <!name string>
	// <!age int>
	// <!is_admin bool>
	// <!created_at datetime>
	// <!meeting_time time>
	// <!meeting_date date>
	// <!any_data any>
	// <!todos string list join="\n">
	// <!weights float32 list join=",">
	// <!flags bool list join="," start="[" end="]">
	// <!names join="\n">
	Var
	RawBlock
)

func (k Kind) String() string {
	switch k {
	case EndTag:
		return "EndTag"
	case OptionalBlock:
		return "OptionalBlock"
	case Title:
		return "Title"
	case Text:
		return "Text"
	case Var:
		return "Var"
	case RawBlock:
		return "RawBlock"
	case Unset:
		return "Unset"
	}
	return "Unknown"
}

func (k Kind) IsTag() bool {
	switch k {
	case
		Var,
		OptionalBlock,
		EndTag:
		return true
	}
	return false
}
