package findeskproc

import "testing"

func TestParsePSOutput(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   State
	}{
		{
			name: "macOS packaged launcher",
			output: `/sbin/launchd
/usr/libexec/UserEventAgent
/Applications/FinDesk.app/Contents/MacOS/FinDesk
`,
			want: Running,
		},
		{
			name: "linux executable",
			output: `systemd
kthreadd
FinDesk
`,
			want: Running,
		},
		{
			name: "case-insensitive name",
			output: `systemd
findesk
`,
			want: Running,
		},
		{
			name:   "FinDeskHelper is another program",
			output: "systemd\nFinDeskHelper\n",
			want:   NotRunning,
		},
		{
			name:   "a backup script is not the launcher",
			output: "bash\nfindesk-backup.sh\n",
			want:   NotRunning,
		},
		{
			name:   "a prefixed name is another program",
			output: "systemd\nMyFinDesk\n",
			want:   NotRunning,
		},
		{
			name:   "a suffixed name is another program",
			output: "systemd\nFinDeskOther\n",
			want:   NotRunning,
		},
		{
			name:   "only other programs",
			output: "launchd\nWindowServer\nTerminal\n",
			want:   NotRunning,
		},
		{
			name:   "empty output",
			output: "",
			want:   Unknown,
		},
		{
			name:   "whitespace-only output",
			output: " \n\t\n",
			want:   Unknown,
		},
		{
			name:   "carriage returns",
			output: "launchd\r\n/Applications/FinDesk.app/Contents/MacOS/FinDesk\r\n",
			want:   Running,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := parsePSOutput([]byte(test.output)); got != test.want {
				t.Errorf("parsePSOutput() = %s, want %s", got, test.want)
			}
		})
	}
}

func TestParseTasklistOutput(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   State
	}{
		{
			name: "Windows packaged launcher",
			output: `"System Idle Process","0","Services","0","8 K"
"explorer.exe","7120","Console","1","120,000 K"
"FinDesk.exe","9132","Console","1","250,000 K"
`,
			want: Running,
		},
		{
			name:   "case-insensitive name",
			output: `"explorer.exe","7120","Console","1","120,000 K"` + "\n" + `"findesk.EXE","9132","Console","1","250,000 K"`,
			want:   Running,
		},
		{
			name:   "FinDeskHelper.exe is another program",
			output: `"FinDeskHelper.exe","9132","Console","1","250,000 K"`,
			want:   NotRunning,
		},
		{
			name:   "a backup script is not the launcher",
			output: `"cmd.exe","9132","Console","1","4,000 K"` + "\n" + `"findesk-backup.bat","9133","Console","1","4,000 K"`,
			want:   NotRunning,
		},
		{
			name:   "a prefixed name is another program",
			output: `"MyFinDesk.exe","9132","Console","1","250,000 K"`,
			want:   NotRunning,
		},
		{
			name:   "only other programs",
			output: `"System","4","Services","0","8 K"` + "\n" + `"explorer.exe","7120","Console","1","120,000 K"`,
			want:   NotRunning,
		},
		{
			name:   "empty output",
			output: "",
			want:   Unknown,
		},
		{
			name:   "whitespace-only output",
			output: " \r\n\t",
			want:   Unknown,
		},
		{
			name:   "malformed CSV",
			output: `"unterminated`,
			want:   Unknown,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := parseTasklistOutput([]byte(test.output)); got != test.want {
				t.Errorf("parseTasklistOutput() = %s, want %s", got, test.want)
			}
		})
	}
}

// TestRealCommandOutputFormatsStayParseable pins one captured sample per OS
// command format. The tests above never run ps or tasklist.
func TestRealCommandOutputFormatsStayParseable(t *testing.T) {
	psFixture := `/sbin/launchd
/usr/sbin/syslogd
/Applications/FinDesk.app/Contents/MacOS/FinDesk
`
	if got := parsePSOutput([]byte(psFixture)); got != Running {
		t.Errorf("captured ps fixture = %s, want running", got)
	}

	tasklistFixture := `"System Idle Process","0","Services","0","8 K"
"FinDesk.exe","12744","Console","1","212,340 K"
`
	if got := parseTasklistOutput([]byte(tasklistFixture)); got != Running {
		t.Errorf("captured tasklist fixture = %s, want running", got)
	}
}
