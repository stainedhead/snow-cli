package humanauth_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/snow-cli/internal/humanauth"
)

func sample() humanauth.Credentials {
	return humanauth.Credentials{Issuer: "i", ClientID: "c", Subject: "s", Scope: "x",
		AccessToken: secretAccess, IDToken: secretID, RefreshToken: secretRefresh, Expiry: time.Unix(1000, 0).UTC()}
}

func storeContract(t *testing.T, s humanauth.Store) {
	t.Helper()
	if _, err := s.Load("p"); !errors.Is(err, humanauth.ErrNotFound) {
		t.Fatalf("empty load = %v", err)
	}
	must(t, s.Delete("p")) // deleting nothing is fine
	must(t, s.Save("p", sample()))
	must(t, s.Save("q", humanauth.Credentials{Subject: "other"}))
	got, err := s.Load("p")
	must(t, err)
	if got != sample() {
		t.Fatalf("round trip: %v", got)
	}
	must(t, s.Delete("p"))
	if _, err := s.Load("p"); !errors.Is(err, humanauth.ErrNotFound) {
		t.Fatal("not deleted")
	}
	if q, err := s.Load("q"); err != nil || q.Subject != "other" {
		t.Fatal("other profile lost")
	}
}

func TestMemoryStore(t *testing.T) {
	s := humanauth.NewMemoryStore()
	storeContract(t, s)
	s.SaveErr = errors.New("boom")
	if s.Save("p", sample()) == nil {
		t.Fatal("SaveErr ignored")
	}
	s.DelErr = errors.New("boom")
	if s.Delete("p") == nil {
		t.Fatal("DelErr ignored")
	}
	if s.Kind() != "memory" {
		t.Fatal(s.Kind())
	}
}

func TestFileStoreContractAndPerms(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "credentials.json")
	s := humanauth.NewFileStore(p)
	storeContract(t, s)
	fi, err := os.Stat(p)
	must(t, err)
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", fi.Mode().Perm())
	}
	di, _ := os.Stat(filepath.Dir(p))
	if di.Mode().Perm() != 0o700 {
		t.Errorf("dir mode %v", di.Mode().Perm())
	}
	ents, _ := os.ReadDir(filepath.Dir(p))
	if len(ents) != 1 {
		t.Errorf("temp files left behind: %v", ents)
	}
	if s.Kind() != "insecure-file" {
		t.Fatal(s.Kind())
	}
}

func TestFileStoreCorruptAndBadDir(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "c.json")
	must(t, os.WriteFile(p, []byte("{not json"), 0o600))
	s := humanauth.NewFileStore(p)
	if _, err := s.Load("p"); err == nil || !strings.Contains(err.Error(), "corrupt") {
		t.Errorf("load: %v", err)
	}
	if s.Save("p", sample()) == nil || s.Delete("p") == nil {
		t.Error("corrupt file must fail save/delete")
	}
	file := filepath.Join(d, "afile")
	must(t, os.WriteFile(file, nil, 0o600))
	if humanauth.NewFileStore(filepath.Join(file, "x", "c.json")).Save("p", sample()) == nil {
		t.Error("mkdir under a file must fail")
	}
	if _, err := humanauth.NewFileStore(filepath.Join(file, "c.json")).Load("p"); err == nil {
		t.Error("read error must surface")
	}
}

func TestStubsFailClosed(t *testing.T) {
	for _, tc := range []struct {
		goos string
		wsl  bool
		want string
	}{{"darwin", false, "macos-keychain"}, {"linux", false, "secret-service"}, {"linux", true, "wsl2-store"}, {"windows", false, "windows-store"}} {
		s := humanauth.SelectStore(tc.goos, tc.wsl, false, "")
		if s.Kind() != tc.want {
			t.Errorf("%s/%v kind %s", tc.goos, tc.wsl, s.Kind())
		}
		_, e1 := s.Load("p")
		e2 := s.Save("p", sample())
		e3 := s.Delete("p")
		for _, err := range []error{e1, e2, e3} {
			var su *humanauth.StoreUnavailableError
			if !errors.As(err, &su) || output.ExitOf(err) != 3 || !strings.Contains(su.Hint(), "--insecure-store") {
				t.Errorf("%s: %v", tc.goos, err)
			}
		}
	}
	if s := humanauth.SelectStore("linux", false, true, "/x/y"); s.Kind() != "insecure-file" {
		t.Error("insecure must select the file store")
	}
	_ = humanauth.DetectWSL()
	if got := humanauth.DefaultInsecurePath("/h"); got != "/h/.config/snow/credentials.json" {
		t.Error(got)
	}
}

func TestCredentialsNeverPrint(t *testing.T) {
	c := sample()
	b, _ := json.Marshal(c)
	b2, _ := json.Marshal(map[string]any{"c": &c})
	for _, s := range []string{
		fmt.Sprint(c), fmt.Sprintf("%v %+v %#v %s %q", c, c, c, c, c), fmt.Sprintf("%v", &c), string(b), string(b2),
	} {
		notContains(t, "credentials format", s)
	}
}

func TestErrorTypes(t *testing.T) {
	oe := &humanauth.OAuthError{Status: 400, Code: "invalid_grant", Description: "bad"}
	if !strings.Contains(oe.Error(), "invalid_grant") || !strings.Contains(oe.Error(), "bad") || output.ExitOf(oe) != 3 {
		t.Error(oe)
	}
	if !strings.Contains((&humanauth.OAuthError{Status: 502}).Error(), "502") {
		t.Error("no code")
	}
	lr := &humanauth.LoginRequiredError{Msg: "m", Err: oe}
	if !errors.Is(lr, oe) || !strings.Contains(lr.Error(), "invalid_grant") || !strings.Contains(lr.Hint(), "snow auth login") || output.ExitOf(lr) != 3 {
		t.Error(lr)
	}
	if (&humanauth.LoginRequiredError{Msg: "m"}).Error() != "m" {
		t.Error("plain")
	}
	if output.ExitOf(&humanauth.PartialError{Msg: "x"}) != 1 {
		t.Error("partial exit")
	}
	if (&humanauth.StoreUnavailableError{Backend: "b", Reason: "r"}).Error() == "" {
		t.Error("store err")
	}
}
