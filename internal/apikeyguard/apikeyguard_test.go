package apikeyguard_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wighawag/anonseed/internal/apikeyguard"
	"github.com/wighawag/anonseed/internal/homewrite"
	"github.com/wighawag/anonseed/internal/seed"
)

// TestLooksRealBenignSet: every value in the benign set (a placeholder a local
// model ignores) is classified NOT real, including case- and whitespace-variants,
// because LooksReal trims and lowercases before the membership check. These
// values mirror anon-pi's BENIGN_API_KEYS.
func TestLooksRealBenignSet(t *testing.T) {
	benign := []string{
		"", "none", "ollama", "no-key", "nokey", "local", "dummy",
		"sk-no-key-required",
		// trim + case variants of benign values are still benign.
		"  ", " none ", "NONE", "Local", "OLLAMA", "  Dummy  ",
	}
	for _, k := range benign {
		if apikeyguard.LooksReal(k) {
			t.Errorf("LooksReal(%q) = true, want false (benign placeholder)", k)
		}
	}
	// The exported placeholder is itself benign, so a seed that writes it is safe.
	if apikeyguard.LooksReal(apikeyguard.PlaceholderAPIKey) {
		t.Errorf("PlaceholderAPIKey %q classified as real; it must be benign", apikeyguard.PlaceholderAPIKey)
	}
}

// TestLooksRealSecrets: anything outside the benign set is treated as a REAL
// secret. Includes provider-shaped keys and an arbitrary token.
func TestLooksRealSecrets(t *testing.T) {
	real := []string{
		"sk-ant-api03-abc123",
		"sk-proj-abcdef",
		"AIzaSyXXXXXXXXXXXXXXXXXXXXXXXXXX",
		"hf_xxxxxxxxxxxxxxxxxxxx",
		"my-real-token",
		"none-but-not-quite", // a superstring of a benign value is NOT benign
		"localhost",          // ditto
	}
	for _, k := range real {
		if !apikeyguard.LooksReal(k) {
			t.Errorf("LooksReal(%q) = false, want true (real-looking secret)", k)
		}
	}
}

// TestGuardBenignPasses: a benign/placeholder key passes the guard whether or not
// force is set (force is irrelevant when there is nothing to refuse).
func TestGuardBenignPasses(t *testing.T) {
	for _, force := range []bool{false, true} {
		if err := apikeyguard.Guard(apikeyguard.PlaceholderAPIKey, force); err != nil {
			t.Errorf("Guard(placeholder, force=%v) = %v, want nil", force, err)
		}
		if err := apikeyguard.Guard("", force); err != nil {
			t.Errorf("Guard(empty, force=%v) = %v, want nil", force, err)
		}
	}
}

// TestGuardRealRefused: a real-looking key WITHOUT force is refused loudly with a
// typed *ErrRealAPIKey whose message names the risk (a credential re-linking the
// anonymized identity to the operator).
func TestGuardRealRefused(t *testing.T) {
	err := apikeyguard.Guard("sk-ant-api03-secret", false)
	if err == nil {
		t.Fatal("Guard(real, force=false) = nil, want a refusal")
	}
	var target *apikeyguard.ErrRealAPIKey
	if !errors.As(err, &target) {
		t.Fatalf("Guard refusal is %T, want *apikeyguard.ErrRealAPIKey", err)
	}
	// The message must name the risk loudly, not just say "invalid".
	msg := strings.ToLower(err.Error())
	for _, want := range []string{"refus", "real", "anonymi"} {
		if !strings.Contains(msg, want) {
			t.Errorf("refusal message %q does not mention %q (must name the risk loudly)", err.Error(), want)
		}
	}
}

// TestGuardForcedRealAllowed: the explicit force flag is the ONE way a real key
// passes, the operator's auditable override.
func TestGuardForcedRealAllowed(t *testing.T) {
	if err := apikeyguard.Guard("sk-ant-api03-secret", true); err != nil {
		t.Errorf("Guard(real, force=true) = %v, want nil (explicit force override)", err)
	}
}

// fakeRunner records chown calls seedhome issues so the acceptance test seeds a
// real home into a temp fixture WITHOUT a real chown (which needs root).
type fakeRunner struct{ calls [][]string }

func (r *fakeRunner) Run(_ context.Context, name string, args ...string) (string, string, error) {
	r.calls = append(r.calls, append([]string{name}, args...))
	return "", "", nil
}

// seedProvider is the shape of a pi provider entry the guard protects: a baseUrl
// and an apiKey. The acceptance test builds a models.json around it exactly as a
// seed would, so the invariant is asserted on the real seeded bytes.
type seedProvider struct {
	API     string `json:"api"`
	BaseURL string `json:"baseUrl"`
	APIKey  string `json:"apiKey"`
}

// guardedSeedModels is a minimal stand-in for what the pi seed does with a key:
// it runs the guard, and only if the guard passes does it place the key into the
// models.json bytes it will seed. A refusal means NO plan (nothing to seed). This
// is the seam the acceptance invariant rests on: a key reaches the SeedPlan ONLY
// after the guard has cleared it.
func guardedSeedModels(candidateKey string, force bool) (seed.SeedPlan, error) {
	if err := apikeyguard.Guard(candidateKey, force); err != nil {
		return seed.SeedPlan{}, err
	}
	body, err := json.Marshal(map[string]any{
		"providers": map[string]seedProvider{
			"local": {API: "openai-completions", BaseURL: "http://127.0.0.1:1234/v1", APIKey: candidateKey},
		},
	})
	if err != nil {
		return seed.SeedPlan{}, err
	}
	return seed.SeedPlan{
		Files: []seed.FileToWrite{{Path: ".pi/agent/models.json", Content: string(body)}},
	}, nil
}

// TestSeededHomeNeverContainsRealCredential is the acceptance-level assertion the
// prd names as a core seam (story 25): after a NORMAL (non-forced) seed of a home
// (a temp fixture, seeded through the real homewrite.Write + anoncore seedhome),
// that home NEVER contains a real-looking credential. We drive it with a REAL key
// and assert the seed is refused so nothing lands, THEN with a placeholder and
// assert the home contains only the benign value. Either way the invariant holds:
// no real credential ever reaches the seeded home on a normal seed.
func TestSeededHomeNeverContainsRealCredential(t *testing.T) {
	realKey := "sk-ant-api03-THIS-IS-A-REAL-SECRET"

	// A NORMAL (non-forced) seed with a REAL key must be refused BEFORE any file
	// is written, so the home is never created with the credential in it.
	if _, err := guardedSeedModels(realKey, false); err == nil {
		t.Fatal("a real key was NOT refused on a normal seed; the invariant is broken at the guard")
	}

	// Now seed a home for real (placeholder key) through the same surface anonseed
	// uses, and assert the seeded bytes on disk contain NO real credential.
	home := t.TempDir()
	plan, err := guardedSeedModels(apikeyguard.PlaceholderAPIKey, false)
	if err != nil {
		t.Fatalf("placeholder seed refused unexpectedly: %v", err)
	}
	r := &fakeRunner{}
	if _, err := homewrite.Write(context.Background(), r, homewrite.Identity{Home: home, Account: "anon"}, plan.Files, false); err != nil {
		t.Fatalf("homewrite.Write: %v", err)
	}

	// Walk EVERY file in the seeded home and assert none carries a real credential.
	// This is the direct invariant: scan the actual on-disk bytes, not just the plan.
	assertNoRealCredentialInHome(t, home, realKey)
}

// assertNoRealCredentialInHome walks the seeded home and fails if any file either
// literally contains the known real secret OR carries an "apiKey" JSON value that
// LooksReal. This asserts the invariant directly against on-disk bytes.
func assertNoRealCredentialInHome(t *testing.T, home, realKey string) {
	t.Helper()
	err := filepath.WalkDir(home, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), realKey) {
			t.Errorf("seeded file %s contains the real secret %q", path, realKey)
		}
		// If the file parses as a pi models.json, assert every provider apiKey is benign.
		var parsed struct {
			Providers map[string]seedProvider `json:"providers"`
		}
		if json.Unmarshal(data, &parsed) == nil {
			for name, p := range parsed.Providers {
				if apikeyguard.LooksReal(p.APIKey) {
					t.Errorf("seeded file %s provider %q carries a real-looking apiKey %q", path, name, p.APIKey)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk seeded home: %v", err)
	}
}
