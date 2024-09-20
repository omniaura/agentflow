package require

import (
	"reflect"
	"testing"
)

func Equal(t *testing.T, expected, actual interface{}) {
	if !reflect.DeepEqual(expected, actual) {
		t.Fatalf("expected %v, got %v", expected, actual)
	}
}

func NoError(t *testing.T, err error) {
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}
