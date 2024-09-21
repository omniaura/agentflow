package testdata

import "github.com/ditto-assistant/agentflow/pkg/ast"

var File1 = ast.MustFile("hello1.af", []byte(file1Content))

const file1Content = `say hello to <!username>`

var File2 = ast.MustFile("hello2.af", []byte(file2Content))

const file2Content = `.title hello user
say hello to <!username>`

var File3 = ast.MustFile("hello3.af", []byte(file3Content))

const file3Content = `.title hello user
say hello to <!username>


.title goodbye user
say goodbye to <!username>`
