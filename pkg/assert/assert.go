package assert

import (
	"fmt"
	"log/slog"
	"os"
	"runtime"
)

func NoError(err error) {
	if err != nil {
		_, file, line, _ := runtime.Caller(1)
		slog.Error(fmt.Sprintf(
			"%s:%d: unexpected error: %v",
			file, line, err,
		))
		os.Exit(1)
	}
}
