package scan

import "testing"

// GnuPG agent keys are binary Libgcrypt s-expressions, not PEM. The dedicated
// pattern must classify them as Skip so they never reach a plaintext backup.
func TestGPGSexpPrivateKeySkipped(t *testing.T) {
	for _, body := range []string{
		"(21:protected-private-key(3:rsa(1:n257:...))",
		"(11:private-key(3:rsa))",
		"(26:shadowed-private-key(3:rsa))",
	} {
		if got := ScanContent("private-keys-v1.d/key.key", body).Action; got != Skip {
			t.Errorf("ScanContent(%.30q) action = %q, want skip", body, got)
		}
	}
}
