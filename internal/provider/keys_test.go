package provider

import "testing"

func TestSeveralKeys(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	if err := Save(Provider{ID: "relay", Name: "Relay", Chat: "https://relay.test/v1", Key: "sk-personal-1111"}); err != nil {
		t.Fatal(err)
	}
	if err := AddKey("relay", "Team", "sk-team-2222"); err != nil {
		t.Fatal(err)
	}
	if err := AddKey("relay", "", "sk-team-2222"); err == nil {
		t.Fatal("the same key was added twice")
	}
	p, _ := Find("relay")
	ks := p.KeyList()
	if p.Key != "sk-personal-1111" || len(ks) != 2 || !ks[0].Active || ks[1].Name != "Team" {
		t.Fatalf("keys %+v", ks)
	}
	if err := RemoveKey("relay", ks[0].ID); err == nil {
		t.Fatal("removed the key in use")
	}
	if err := RenameKey("relay", ks[0].ID, "Personal"); err != nil {
		t.Fatal(err)
	}
	if err := UseKey("relay", ks[1].ID); err != nil {
		t.Fatal(err)
	}
	p, _ = Find("relay")
	if p.Key != "sk-team-2222" || p.KeyName != "Team" || len(p.Keys) != 1 || p.Keys[0] != (KeyAccount{"Personal", "sk-personal-1111"}) {
		t.Fatalf("after use: %+v %q %+v", p.Key, p.KeyName, p.Keys)
	}
	if err := RemoveKey("relay", keyID("sk-personal-1111")); err != nil {
		t.Fatal(err)
	}
	if p, _ = Find("relay"); len(p.Keys) != 0 || p.Key != "sk-team-2222" {
		t.Fatalf("after remove: %+v", p)
	}
}
