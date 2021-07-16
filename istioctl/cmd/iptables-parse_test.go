package cmd

import "testing"

func TestParseIptablesLine(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			"simple",
			`Jul 16 14:44:56 howardjohn-glaptop2.roam.corp.google.com kernel: [790712.172788] IPT03 shell-7b56cdf894-8fd5h:IN=eth0 OUT= MAC=6e:eb:d7:07:52:19:8e:d1:43:47:3b:88:08:00 SRC=10.244.0.1 DST=10.244.0.37 LEN=60 TOS=0x00 PREC=0x00 TTL=64 ID=11514 DF PROTO=TCP SPT=51562 DPT=15021 WINDOW=64240 RES=0x00 SYN URGP=0`,
			"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseIptablesLine(tt.input); got != tt.want {
				t.Errorf("parseIptablesLine() = %v, want %v", got, tt.want)
			}
		})
	}
}
