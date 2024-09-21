package errs

import (
	"unique"
)

// TODO INTEGRATE WITH SLOG.ATTR

func New[M ~string](msg M) Error {
	return Error{
		msg: unique.Make(message(msg)),
	}
}

// TODO USE ERROR STACK
func (e Error) Error() string {
	return e.Msg().Error()
}

type Error struct {
	msg   unique.Handle[message]
	stack []fmtErr
}

func (e Error) Msg() message {
	return e.msg.Value()
}

type message string

func (m message) String() string {
	return string(m)
}
func (m message) Error() string {
	return string(m)
}

type fmtErr struct {
	err  error
	msg  string
	args []any
}

// TODO CAPTURE RUNTIME.CALLER FOR ALL BELOW

func (e Error) S(msg string) Error {
	e.stack = append(e.stack, fmtErr{msg: msg})
	return e
}

func (e Error) F(msg string, args ...any) Error {
	e.stack = append(e.stack, fmtErr{msg: msg, args: args})
	return e
}

func (e Error) E(err error) Error {
	e.stack = append(e.stack, fmtErr{err: err})
	return e
}

func (e Error) ES(err error, msg string) Error {
	e.stack = append(e.stack, fmtErr{err: err, msg: msg})
	return e
}

func (e Error) EF(err error, fmt string, args ...any) Error {
	e.stack = append(e.stack, fmtErr{err: err, msg: fmt, args: args})
	return e
}
