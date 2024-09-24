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
package token_test

import (
	"testing"

	"github.com/omniaura/agentflow/pkg/assert/require"
	"github.com/omniaura/agentflow/pkg/token"
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
