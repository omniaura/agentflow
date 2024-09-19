package tokenizer_test

import (
	"testing"

	"github.com/ditto-assistant/agentflow/pkg/tokenizer"
	"github.com/stretchr/testify/require"
)

func TestTokenSlice_Equal(t *testing.T) {
	testcases := []struct {
		name string
		a    tokenizer.TokenSlice
		b    tokenizer.TokenSlice
		want bool
	}{
		{
			name: "empty",
			a:    tokenizer.TokenSlice{},
			b:    tokenizer.TokenSlice{},
			want: true,
		},
		{
			name: "equal",
			a: tokenizer.TokenSlice{
				{
					Kind:  tokenizer.KindTitle,
					Start: 0,
					End:   10,
				},
			},
			b: tokenizer.TokenSlice{
				{
					Kind:  tokenizer.KindTitle,
					Start: 0,
					End:   10,
				},
			},
			want: true,
		},
		{
			name: "not equal",
			a: tokenizer.TokenSlice{
				{
					Kind:  tokenizer.KindTitle,
					Start: 0,
					End:   10,
				},
			},
			b: tokenizer.TokenSlice{
				{
					Kind:  tokenizer.KindTitle,
					Start: 0,
					End:   11,
				},
			},
			want: false,
		},
		{
			name: "not equal kind",
			a: tokenizer.TokenSlice{
				{
					Kind:  tokenizer.KindTitle,
					Start: 0,
					End:   10,
				},
			},
			b: tokenizer.TokenSlice{
				{
					Kind:  tokenizer.KindText,
					Start: 0,
					End:   10,
				},
			},
			want: false,
		},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.a.Equal(tc.b))
		})
	}
}
