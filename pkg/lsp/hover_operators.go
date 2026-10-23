package lsp

import (
	"fmt"
	"strings"

	"github.com/omniaura/agentflow/pkg/token/kind"
)

type OperatorInfo struct {
	Name        string
	Description string
	Examples    []string
}

var operatorDocs = map[string]OperatorInfo{
	"eq": {
		Name:        "eq (equals)",
		Description: "Checks if two values are equal. Returns true if the left operand equals the right operand.",
		Examples: []string{
			`if user.age eq 18`,
			`if status eq "active"`,
			`if count eq 0`,
		},
	},
	"ne": {
		Name:        "ne (not equals)",
		Description: "Checks if two values are not equal. Returns true if the left operand does not equal the right operand.",
		Examples: []string{
			`if user.status ne "inactive"`,
			`if result ne null`,
			`if count ne 0`,
		},
	},
	"gt": {
		Name:        "gt (greater than)",
		Description: "Checks if the left value is greater than the right value. Works with numbers, strings (lexicographic), and dates.",
		Examples: []string{
			`if user.age gt 21`,
			`if temperature gt 32.0`,
			`if name gt "apple"`,
		},
	},
	"lt": {
		Name:        "lt (less than)",
		Description: "Checks if the left value is less than the right value. Works with numbers, strings (lexicographic), and dates.",
		Examples: []string{
			`if user.age lt 65`,
			`if temperature lt 0`,
			`if priority lt "high"`,
		},
	},
	"gte": {
		Name:        "gte (greater than or equal)",
		Description: "Checks if the left value is greater than or equal to the right value. Returns true if left >= right.",
		Examples: []string{
			`if user.age gte 18`,
			`if score gte 80.0`,
			`if version gte "2.0"`,
		},
	},
	"lte": {
		Name:        "lte (less than or equal)",
		Description: "Checks if the left value is less than or equal to the right value. Returns true if left <= right.",
		Examples: []string{
			`if user.age lte 100`,
			`if usage lte limit`,
			`if priority lte "medium"`,
		},
	},
}

func (d *Document) GetOperatorAt(pos Position) *OperatorInfo {
	off := d.positionToOffset(pos)
	for _, tok := range d.Tokens {
		if tok.Kind != kind.Operator || off < tok.Start || off >= tok.End {
			continue
		}
		if info, ok := operatorDocs[string(tok.Get(d.Content))]; ok {
			return &info
		}
	}
	return nil
}
func formatOperatorHover(info *OperatorInfo) string {
	var content strings.Builder

	content.WriteString(fmt.Sprintf("**%s**\n\n", info.Name))
	content.WriteString(fmt.Sprintf("%s\n\n", info.Description))

	if len(info.Examples) > 0 {
		content.WriteString("**Examples:**\n")
		for _, example := range info.Examples {
			content.WriteString(fmt.Sprintf("```agentflow\n%s\n```\n", example))
		}
	}

	return content.String()
}
