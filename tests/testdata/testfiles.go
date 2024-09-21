package testdata

const (
	NoVarsNoTitle = `say hello to the user!`

	OneVarNoTitle = `say hello to <!username>`

	OneVarWithTitle = `.title hello user
say hello to <!username>`

	TwoPromptsWithVars = `.title hello user
say hello to <!username>


.title goodbye user
say goodbye to <!username>`
)
