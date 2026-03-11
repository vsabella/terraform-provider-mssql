package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func Test_hasLoginSidMismatch(t *testing.T) {
	tests := []struct {
		name        string
		configSid   types.String
		existingSid string
		want        bool
	}{
		{
			name:        "null config sid",
			configSid:   types.StringNull(),
			existingSid: "0xabc123",
			want:        false,
		},
		{
			name:        "unknown config sid",
			configSid:   types.StringUnknown(),
			existingSid: "0xabc123",
			want:        false,
		},
		{
			name:        "empty config sid",
			configSid:   types.StringValue(""),
			existingSid: "0xabc123",
			want:        false,
		},
		{
			name:        "empty existing sid",
			configSid:   types.StringValue("0xabc123"),
			existingSid: "",
			want:        false,
		},
		{
			name:        "case-insensitive match",
			configSid:   types.StringValue("0xABC123"),
			existingSid: "0xabc123",
			want:        false,
		},
		{
			name:        "sid mismatch",
			configSid:   types.StringValue("0xabc123"),
			existingSid: "0xdef456",
			want:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasLoginSidMismatch(tt.configSid, tt.existingSid)
			if got != tt.want {
				t.Fatalf("hasLoginSidMismatch()=%v, want %v", got, tt.want)
			}
		})
	}
}
