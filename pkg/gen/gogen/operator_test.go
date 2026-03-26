package gogen

import "testing"

func TestConvertWordOperatorToGoRejectsUnknownOperator(t *testing.T) {
	_, err := convertWordOperatorToGo("or")
	if err == nil {
		t.Fatal("expected error for unsupported operator")
	}
}
