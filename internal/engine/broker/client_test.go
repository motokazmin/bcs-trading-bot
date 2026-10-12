package broker

import (
	"encoding/base64"
	"testing"
	"time"
)

// Срок refresh token читается из exp: по нему старт предупреждает об
// истечении, а Connect объясняет "Token is not active".
func TestRefreshTokenExpiryЧитаетExp(t *testing.T) {
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"typ":"Refresh","exp":1791618899}`))
	exp, ok := RefreshTokenExpiry("hdr." + payload + ".sig")
	if !ok || !exp.Equal(time.Unix(1791618899, 0)) {
		t.Fatalf("exp = %v, ok = %v", exp, ok)
	}
	if _, ok := RefreshTokenExpiry("не-jwt"); ok {
		t.Fatal("не-JWT разобран как токен")
	}
}
