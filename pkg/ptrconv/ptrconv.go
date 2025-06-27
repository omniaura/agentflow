package ptrconv

func Str[T ~string](s T) *T    { return &s }
func Int[T ~int](i T) *T       { return &i }
func Int32[T ~int32](i T) *T   { return &i }
func Int64[T ~int64](i T) *T   { return &i }
func Uint[T ~uint](i T) *T     { return &i }
func Uint32[T ~uint32](i T) *T { return &i }
func Uint64[T ~uint64](i T) *T { return &i }
func Bool[T ~bool](b T) *T     { return &b }

func StrNil[T ~string](s T) *T {
	if s == "" {
		return nil
	}
	return &s
}
