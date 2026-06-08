package settings

import "testing"

func TestResolvePrecedence(t *testing.T) {
	const key = "orient.recency_days" // default "30", env RILL_ORIENT_RECENCY_DAYS

	// no env, no db -> default
	t.Setenv("RILL_ORIENT_RECENCY_DAYS", "")
	s := &Service{dbVals: map[string]string{}}
	if v, src := s.resolve(key); v != "30" || src != "default" {
		t.Fatalf("default: got (%q,%q), want (30,default)", v, src)
	}

	// db override -> db wins over default
	s.dbVals[key] = "45"
	if v, src := s.resolve(key); v != "45" || src != "db" {
		t.Fatalf("db: got (%q,%q), want (45,db)", v, src)
	}
	if got := s.OrientRecencyDays(); got != 45 {
		t.Fatalf("typed getter: got %d, want 45", got)
	}

	// env set -> env wins over db AND locks the field
	t.Setenv("RILL_ORIENT_RECENCY_DAYS", "7")
	if v, src := s.resolve(key); v != "7" || src != "env" {
		t.Fatalf("env: got (%q,%q), want (7,env)", v, src)
	}
	if !envPinned(key) {
		t.Fatal("env-set key should be pinned/locked")
	}
}

func TestListNeverLeaksSecrets(t *testing.T) {
	t.Setenv("RILL_OIDC_CLIENT_SECRET", "super-secret-value")
	s := &Service{dbVals: map[string]string{}}
	for _, r := range s.List() {
		if r.Secret {
			if r.Value != "" {
				t.Errorf("secret %s leaked value %q", r.Key, r.Value)
			}
			if r.Key == "oidc.client_secret" && !r.Configured {
				t.Errorf("secret %s should report Configured=true when env-set", r.Key)
			}
		}
	}
}

func TestSetRejectsNonEditableAndEnvLocked(t *testing.T) {
	s := &Service{dbVals: map[string]string{}}

	// read-only key
	if err := s.Set(nil, "auth.trusted_proxy_ips", "1.2.3.4", "tester"); err == nil {
		t.Error("expected error setting a read-only key")
	}
	// unknown key
	if err := s.Set(nil, "nope.nope", "x", "tester"); err == nil {
		t.Error("expected error for unknown key")
	}
	// env-locked editable key
	t.Setenv("RILL_ORIENT_RECENCY_DAYS", "30")
	if err := s.Set(nil, "orient.recency_days", "45", "tester"); err == nil {
		t.Error("expected env-lock error")
	}
}

func TestValidate(t *testing.T) {
	recency, _ := SettingMeta("orient.recency_days")
	if err := validate(recency, "0"); err == nil {
		t.Error("0 below min should fail")
	}
	if err := validate(recency, "abc"); err == nil {
		t.Error("non-int should fail")
	}
	if err := validate(recency, "30"); err != nil {
		t.Errorf("30 should be valid: %v", err)
	}
	mode, _ := SettingMeta("mcp.compact_tools")
	if err := validate(mode, "bogus"); err == nil {
		t.Error("bad enum should fail")
	}
	if err := validate(mode, "names"); err != nil {
		t.Errorf("valid enum failed: %v", err)
	}
}
