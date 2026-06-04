package scan

import (
	"strings"
	"testing"
)

func TestProviderTokenPatterns(t *testing.T) {
	cases := []struct{ name, secret string }{
		{"digitalocean", "token: dop_v1_" + strings.Repeat("a", 64)},
		{"vault", "VAULT=hvs.CAESIJ0aBcDeFgHiJkLmNoPqRsTu"},
		{"pulumi", "PULUMI_ACCESS_TOKEN=pul-" + strings.Repeat("a", 40)},
		{"flyio", "FLY_API_TOKEN=fm2_aBcDeFgHiJkLmNoPqRsTuVwX"},
		{"azure-sas", "https://x.blob.core.windows.net/?sig=" + strings.Repeat("A", 44)},
		{"pgpass", "localhost:5432:mydb:myuser:s3cr3tpass"},
	}
	for _, c := range cases {
		r := ScanContent("f", c.secret)
		if r.Action != Redact {
			t.Errorf("%s: action = %q, want redact (no pattern matched %q)", c.name, r.Action, c.secret)
		}
	}

	// The redactor masks the matched token.
	out := ApplyRedactions("token: dop_v1_"+strings.Repeat("a", 64), ScanContent("f", "token: dop_v1_"+strings.Repeat("a", 64)))
	if strings.Contains(out, "dop_v1_") {
		t.Errorf("DigitalOcean token not redacted: %q", out)
	}
}
