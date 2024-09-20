package token_test

import (
	"testing"

	"github.com/ditto-assistant/agentflow/pkg/assert/require"
	"github.com/ditto-assistant/agentflow/pkg/token"
)

func TestTokenSlice_Equal(t *testing.T) {
	testcases := []struct {
		name string
		a    token.Slice
		b    token.Slice
		want bool
	}{
		{
			name: "empty",
			a:    token.Slice{},
			b:    token.Slice{},
			want: true,
		},
		{
			name: "equal",
			a: token.Slice{
				{
					Kind:  token.KindTitle,
					Start: 0,
					End:   10,
				},
			},
			b: token.Slice{
				{
					Kind:  token.KindTitle,
					Start: 0,
					End:   10,
				},
			},
			want: true,
		},
		{
			name: "not equal",
			a: token.Slice{
				{
					Kind:  token.KindTitle,
					Start: 0,
					End:   10,
				},
			},
			b: token.Slice{
				{
					Kind:  token.KindTitle,
					Start: 0,
					End:   11,
				},
			},
			want: false,
		},
		{
			name: "not equal kind",
			a: token.Slice{
				{
					Kind:  token.KindTitle,
					Start: 0,
					End:   10,
				},
			},
			b: token.Slice{
				{
					Kind:  token.KindText,
					Start: 0,
					End:   10,
				},
			},
			want: false,
		},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.want {
				require.Equal(t, tc.a, tc.b)
			} else {
				require.NotEqual(t, tc.a, tc.b)
			}
		})
	}
}
