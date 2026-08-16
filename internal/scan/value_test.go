package scan

import "testing"

// Every "code" case below was a real HIGH finding this scanner reported against
// an unmodified powerlevel10k install. Flagging a shell theme is not a harmless
// extra: it is how a user learns that HIGH means nothing.
func TestKeywordRulesIgnoreCodeThatMentionsTheWord(t *testing.T) {
	code := []string{
		"token ==",                 // comparison, not assignment
		"token=$tokens[1]",         // variable reference
		"token::=${(Q)${~token}}}", // zsh, doubled delimiter
		"GOOGLE_APPLICATION_CREDENTIALS:+$commands", // ${VAR:+x} expansion
		"password=${PASSWORD:-}",                    // ${VAR:-} expansion
		"secret: {",                                 // opens a block
		"api_key = <your-key-here>",                 // documentation
		"TOKEN=changeme",                            // sample config
		"password=",                                 // nothing assigned
	}
	for _, line := range code {
		if valueLooksReal(line) {
			t.Errorf("reported as a secret, but it is code or a placeholder: %q", line)
		}
	}
}

func TestKeywordRulesStillCatchRealSecrets(t *testing.T) {
	real := []string{
		"AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		"password=hunter2",
		`"token": "ghp_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"`,
		"_authToken=npm_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"api_key: sk-proj-abc123def456",
	}
	for _, line := range real {
		if !valueLooksReal(line) {
			t.Errorf("a real secret was filtered out: %q", line)
		}
	}
}

// Compiled zsh, object files and images match rules out of arbitrary bytes.
// GnuPG's binary private key is the exception worth keeping.
func TestBinaryContentKeepsOnlyKeyMaterial(t *testing.T) {
	binary := "\x00\x01\x02 token=abcdefghijklmnop \x00 password=hunter2"
	if got := ScanContent("compiled.zwc", binary); len(got.Findings) != 0 {
		t.Errorf("keyword rules should not fire on binary, got %d finding(s)", len(got.Findings))
	}

	// A GnuPG agent key is binary and must still be caught.
	key := "\x00\x00(21:protected-private-key(3:rsa"
	got := ScanContent("private-keys-v1.d/ABC.key", key)
	if len(got.Findings) == 0 {
		t.Fatal("binary GnuPG private key went undetected")
	}
	if got.Action != Skip {
		t.Errorf("a private key must never be written out: action = %v, want Skip", got.Action)
	}
}

func TestTextContentIsUnaffected(t *testing.T) {
	got := ScanContent("config", "password=hunter2\n")
	if len(got.Findings) == 0 {
		t.Fatal("plain text scanning regressed")
	}
}
