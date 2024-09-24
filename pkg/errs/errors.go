/*
Copyright © 2024 Omni Aura peyton@omniaura.co

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
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
