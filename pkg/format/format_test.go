package format

import (
	"bytes"
	"testing"

	"github.com/omniaura/agentflow/pkg/assert/require"
)

func TestFormatFixtures(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "already formatted",
			in: `.title System Prompt

Hello <!user.name>.

<?user.premium bool>
Premium guidance.
<else>
Default guidance.
</user.premium>
`,
			want: `.title System Prompt

Hello <!user.name>.

<?user.premium bool>
Premium guidance.
<else>
Default guidance.
</user.premium>
`,
		},
		{
			name: "tool sections",
			in:   ".title System\nUse <! tool >.\n\n\n.tool lookup   \n.param ids []int   The ids.  \n.maxbytes 10 \n  Look up <!not a var>.  \n\n.tool ping\nPing.\n\n\n",
			want: ".title System\n\nUse <!tool>.\n\n.tool lookup\n.param ids []int   The ids.\n.maxbytes 10\n  Look up <!not a var>.\n\n.tool ping\nPing.\n",
		},
		{
			name: "tab after tool",
			in:   ".title System\nHi.\n.tool\tlookup\n.param\tids []int\nLook up.\n",
			want: ".title System\n\nHi.\n\n.tool\tlookup\n.param\tids []int\nLook up.\n",
		},
		{
			name: "messy variable spacing",
			in:   ".title   Greeting   \n\nHello <!  user.name   string  > and <!\tcount\tint\t>.\n",
			want: `.title Greeting

Hello <!user.name string> and <!count int>.
`,
		},
		{
			name: "messy conditional spacing",
			in: `.title Access

<?   plan.depth   gte   3   >
Plan first.
<else  >
Act directly.
</  plan.depth  >
`,
			want: `.title Access

<?plan.depth gte 3>
Plan first.
<else>
Act directly.
</plan.depth>
`,
		},
		{
			name: "multiple titles",
			in: `.title One
Body one.



.title    Two


Body two.
`,
			want: `.title One

Body one.

.title Two

Body two.
`,
		},
		{
			name: "trailing whitespace and final newline",
			in:   ".title Demo\r\n\r\nLine with spaces.   \r\nLast line\t",
			want: ".title Demo\n\nLine with spaces.   \nLast line\n",
		},
		{
			name: "body whitespace kept except around the body",
			in:   ".title Notes  \n\n\n  indented\nhard break  \n   \nend  \n\n.title Next\nx\n",
			want: ".title Notes\n\n  indented\nhard break  \n   \nend\n\n.title Next\n\nx\n",
		},
		{
			name: "preserves comments",
			in:   ".title   Demo\n# internal note   \nHello <! name >\n",
			want: ".title Demo\n\n# internal note\nHello <!name>\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Format(tt.name+".af", []byte(tt.in))
			require.NoError(t, err)
			if string(got) != tt.want {
				t.Fatalf("unexpected formatted output:\nwant:\n%q\ngot:\n%q", tt.want, string(got))
			}

			again, err := Format(tt.name+".af", got)
			require.NoError(t, err)
			if !bytes.Equal(got, again) {
				t.Fatalf("formatting is not idempotent:\nonce:\n%q\ntwice:\n%q", string(got), string(again))
			}
		})
	}
}
