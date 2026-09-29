package services

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// Neither the stored model nor the API view may ever carry a secret, in
// plaintext or ciphertext, under any key.
func TestCredentialViewHasNoSecrets(t *testing.T) {
	c := models.SNMPCredential{Name: "v3", Version: "3", Username: "mon",
		Community: "CIPHER-COMMUNITY", AuthProtocol: "SHA", AuthPassword: "CIPHER-AUTH",
		PrivProtocol: "AES", PrivPassword: "CIPHER-PRIV"}
	for name, v := range map[string]any{"model": c, "view": ViewOf(c, 2, "HQ")} {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		s := string(b)
		for _, bad := range []string{"CIPHER-", `"community"`, `"auth_password"`, `"priv_password"`} {
			if strings.Contains(s, bad) {
				t.Errorf("%s JSON contains %s: %s", name, bad, s)
			}
		}
	}
	v := ViewOf(c, 2, "HQ")
	if !v.HasCommunity || !v.HasAuthPassword || !v.HasPrivPassword || v.UsedBy != 2 || v.SiteName != "HQ" {
		t.Errorf("view flags: %+v", v)
	}
}
