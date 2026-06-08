package memory

import "testing"

func TestSafeRecordID(t *testing.T) {
	valid := []string{
		"memory:abc123",
		"person:ada_lovelace",
		"uses:e4884160a3146f203eccc",
		"memory:`20260529T165249.924143496Z`", // backtick-wrapped timestamp (has a dot)
		"person:`Alice Jones`",                // backtick-wrapped, embedded space ok
		"concept:x",
		"app_setting:`orient.recency_days`",
	}
	for _, id := range valid {
		if err := safeRecordID(id); err != nil {
			t.Errorf("expected VALID, got error for %q: %v", id, err)
		}
	}

	// Injection / malformed attempts — every one must be rejected.
	invalid := []string{
		"uses:x; DELETE memory; --",          // statement injection
		"memory:`x`; DELETE memory; --`",     // backtick breakout via embedded backtick
		"person:alice`; DROP",                // stray backtick, not properly wrapped
		"memory:x OR 1=1",                    // space + operator
		"memory:`a`b`",                       // embedded backtick inside wrap
		"memory:x'",                          // quote
		"no_colon",                           // no table prefix
		":empty_table",                       // empty table
		"memory:",                            // empty id
		"MEMORY:x",                           // uppercase table
		"mem ory:x",                          // space in table
		"memory:x y",                         // space in id
		"works_on:abc; SELECT * FROM auth_token", // exfil attempt
	}
	for _, id := range invalid {
		if err := safeRecordID(id); err == nil {
			t.Errorf("expected REJECTED, but %q passed", id)
		}
	}
}
